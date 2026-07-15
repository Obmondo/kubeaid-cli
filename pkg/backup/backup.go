// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

// Package backup reaches the in-cluster backup-exporter Service through
// the Kubernetes apiserver Service proxy, fetches its /backups JSON
// payload and renders a backup-health report.
package backup

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/Obmondo/kubeaid-cli/pkg/utils/logger"
)

// OutputFormat selects how the fetched backup status is rendered.
type OutputFormat string

// Options carries everything `backup status` needs to reach the
// exporter and render its response. Out is where rendered output is
// written (os.Stdout for the command, a buffer in tests).
type Options struct {
	Kubeconfig string
	Context    string
	Namespace  string
	Service    string
	Port       string
	Output     OutputFormat
	Out        io.Writer
}

const (
	// OutputTable is the default compact, one-glyph-per-method view.
	OutputTable OutputFormat = "table"
	// OutputWide expands every age/interval column.
	OutputWide OutputFormat = "wide"
	// OutputJSON prints the raw /backups payload unmodified.
	OutputJSON OutputFormat = "json"
)

const (
	// DefaultNamespace is the exporter's default deployment namespace.
	DefaultNamespace = "monitoring"
	// DefaultService is the exporter's default Service name.
	DefaultService = "obmondo-backup-exporter"
	// DefaultPort is the exporter's default HTTP port.
	DefaultPort = "8080"
)

// backupsPath is the exporter endpoint that returns the JSON report.
const backupsPath = "/backups"

// fetchBackupsTimeout bounds the apiserver Service-proxy round-trip for a
// single /backups fetch, so a hung exporter or apiserver fails fast.
const fetchBackupsTimeout = 15 * time.Second

// fetchBackups reaches the exporter Service through the apiserver
// Service proxy and returns the raw /backups body. It is a package-level
// func var so tests can stub it (mirrors the DI seam in
// pkg/utils/kubernetes). It uses the standard kubeconfig loading rules
// (honoring $KUBECONFIG, ~/.kube/config and the current context) — this
// is a normal kubectl-style client, unrelated to cluster bootstrap.
var fetchBackups = func(
	ctx context.Context,
	kubeconfig, kubecontext, namespace, service, port string,
) ([]byte, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfig != "" {
		loadingRules.ExplicitPath = kubeconfig
	}

	overrides := &clientcmd.ConfigOverrides{}
	if kubecontext != "" {
		overrides.CurrentContext = kubecontext
	}

	cfg, err := clientcmd.
		NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides).
		ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("failed building kube client config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed creating kube client: %w", err)
	}

	fetchCtx, cancel := context.WithTimeout(ctx, fetchBackupsTimeout)
	defer cancel()

	raw, err := clientset.CoreV1().
		Services(namespace).
		ProxyGet("http", service, port, backupsPath, nil).
		DoRaw(fetchCtx)
	if err != nil {
		return nil, fmt.Errorf(
			"failed fetching %s from service %s/%s:%s (is the exporter deployed and does your user hold get on services/proxy?): %w",
			backupsPath, namespace, service, port, err,
		)
	}

	return raw, nil
}

// Status fetches the exporter's /backups payload and writes the rendered
// report to opts.Out. For OutputJSON the raw payload is written
// unmodified; otherwise it is parsed, grouped and rendered as tables.
func Status(ctx context.Context, opts Options) error {
	// Validate the output format before any network round-trip, so a bad
	// -o fails fast rather than after (or masked by) a live fetch.
	switch opts.Output {
	case OutputTable, OutputWide, OutputJSON:
	default:
		return fmt.Errorf("invalid output format %q (want table, wide or json)", opts.Output)
	}

	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("namespace", opts.Namespace),
		slog.String("service", opts.Service),
		slog.String("port", opts.Port),
	})
	slog.DebugContext(ctx, "Fetching backup status from the backup-exporter service")

	raw, err := fetchBackups(ctx, opts.Kubeconfig, opts.Context, opts.Namespace, opts.Service, opts.Port)
	if err != nil {
		return err
	}

	switch opts.Output {
	case OutputJSON:
		return writeJSON(opts.Out, raw)

	case OutputTable, OutputWide:
		report, err := Parse(raw)
		if err != nil {
			return err
		}
		if _, err := io.WriteString(opts.Out, report.Render(opts.Output == OutputWide)); err != nil {
			return fmt.Errorf("failed writing table output: %w", err)
		}
		return nil

	default:
		return fmt.Errorf("invalid output format %q (want table, wide or json)", opts.Output)
	}
}

// writeJSON writes the raw payload verbatim, appending a single trailing
// newline for terminal cleanliness only when one is not already present.
func writeJSON(out io.Writer, raw []byte) error {
	if _, err := out.Write(raw); err != nil {
		return fmt.Errorf("failed writing JSON output: %w", err)
	}
	if len(raw) == 0 || raw[len(raw)-1] != '\n' {
		if _, err := io.WriteString(out, "\n"); err != nil {
			return fmt.Errorf("failed writing JSON output: %w", err)
		}
	}
	return nil
}
