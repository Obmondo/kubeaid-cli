// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package parser

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/Obmondo/kubeaid-cli/pkg/config/clusterdir"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/globals"
)

// ResolveConfigsDirectory resolves the configs directory from a local path
// or --cluster-name; with neither flag, the one cluster that has a saved
// config is used — that is the common case, an operator who just ran
// `config generate` and manages a single cluster, and there is nothing to
// guess between.
//
// With two or more saved clusters the run is refused: cluster commands
// create and destroy real infrastructure, so which cluster is never
// guessed. A stale ./outputs/configs in the working directory is likewise
// never picked up silently — an operator who wants a custom location says
// so with --configs-directory.
func ResolveConfigsDirectory(ctx context.Context) error {
	// An explicit --configs-directory always wins; --cluster-name only fills
	// in when no directory was given, so neither flag can silently override
	// the other.
	if !UsingDefaultConfigsDirectory() {
		return nil
	}

	clusterName := globals.ClusterName
	if clusterName == "" {
		saved := clusterdir.List()
		if len(saved) != 1 {
			return noConfigSourceError(saved)
		}
		clusterName = saved[0]
		slog.InfoContext(ctx, "Using the only cluster with a saved config",
			slog.String("cluster", clusterName))
		// Straight to the terminal as well: outside debug mode stdout only
		// carries ERROR, and the operator must see which cluster this run
		// is about to act on.
		fmt.Fprintf(os.Stderr, "Using cluster %q — the only one with a saved config\n", clusterName)
	}

	directory, err := clusterdir.For(clusterName)
	if err != nil {
		return err
	}
	globals.ConfigsDirectory = directory

	slog.InfoContext(ctx, "Resolved configs directory from cluster name",
		slog.String("cluster", clusterName),
		slog.String("path", directory),
	)
	return nil
}

// noConfigSourceError names the clusters that already have a config on disk,
// so the operator's next command is a copy-paste rather than a search.
func noConfigSourceError(available []string) error {
	if len(available) == 0 {
		return fmt.Errorf(
			"no config source: run `kubeaid-cli config generate` first, then re-run with --%s <name> (or --%s <path>)",
			constants.FlagNameClusterName, constants.FlagNameConfigsDirectory,
		)
	}

	return fmt.Errorf(
		"no config source — several clusters have a saved config, pass --%s <name> (or --%s <path>) to say which one:\n  %s",
		constants.FlagNameClusterName, constants.FlagNameConfigsDirectory,
		strings.Join(available, "\n  "),
	)
}

// UsingDefaultConfigsDirectory reports whether --configs-directory was left
// alone. Compared by value rather than cobra's Changed() so that pkg/ code
// can ask without importing cmd/; passing the default explicitly is
// indistinguishable, which is harmless because it resolves the same way.
func UsingDefaultConfigsDirectory() bool {
	return globals.ConfigsDirectory == constants.FlagNameConfigsDirectoryDefaultValue
}
