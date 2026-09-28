// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package root

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-cli/pkg/siem/metrics"
	"github.com/Obmondo/kubeaid-cli/pkg/siem/reconcile"
	"github.com/Obmondo/kubeaid-cli/pkg/siem/report"
)

const shutdownTimeout = 5 * time.Second

// serve is the long-running mode: reconcile and probe every
// flags.interval, and serve the metrics until SIGTERM/SIGINT.
func serve(cmd *cobra.Command, opts reconcile.Options) error {
	ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	rec := metrics.New()
	ln, err := net.Listen("tcp", flags.metricsAddr)
	if err != nil {
		return fmt.Errorf("metrics listener: %w", err)
	}
	srv := &http.Server{Handler: rec.Handler(), ReadHeaderTimeout: 10 * time.Second}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	defer func() {
		sctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "serving metrics on %s, reconciling every %s\n", ln.Addr(), flags.interval)

	ticker := time.NewTicker(flags.interval)
	defer ticker.Stop()
	for {
		tick(ctx, cmd.OutOrStdout(), rec, opts, flags.probe)
		select {
		case <-ctx.Done():
			return nil
		case err := <-errc:
			if errors.Is(err, http.ErrServerClosed) {
				return nil
			}
			return fmt.Errorf("metrics server: %w", err)
		case <-ticker.C:
		}
	}
}

// tick runs one reconcile (bounded by flags.timeout), prints and
// records it, then probes the health.
func tick(ctx context.Context, out io.Writer, rec *metrics.Recorder, opts reconcile.Options, probe bool) {
	rctx, cancel := context.WithTimeout(ctx, flags.timeout)
	defer cancel()
	start := time.Now()
	results := reconcile.Run(rctx, opts)
	end := time.Now()
	if ctx.Err() != nil {
		return // shutting down: a cut-short run is not a result
	}
	_, _ = fmt.Fprintf(out, "--- run at %s\n", end.UTC().Format(time.RFC3339))
	_ = report.Print(out, results, opts.DryRun)
	rec.ObserveRun(results, end, end.Sub(start), opts.DryRun)
	if probe {
		pctx, pcancel := context.WithTimeout(ctx, flags.timeout)
		defer pcancel()
		rec.ObserveHealth(reconcile.Probe(pctx, opts))
	}
}
