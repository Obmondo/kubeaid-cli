// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"fmt"
	"strings"

	"github.com/Obmondo/kubeaid-cli/pkg/config/clusterdir"
	"github.com/Obmondo/kubeaid-cli/pkg/config/parser"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/globals"
)

// Prepare resolves the config source and parses the config files. Returns
// a cleanup function the caller runs at process finalization to remove
// temp config dirs created for stdin-based configs.
//
// Does NOT run the interactive prompt — that's the dedicated job of
// `kubeaid-cli config generate`. The earlier silent-prompt fallback
// here meant `cluster bootstrap` could surprise an operator with a TUI
// when they expected a parse failure, and the split also makes the
// command boundaries cleaner: `config generate` writes, `cluster
// bootstrap` reads.
func Prepare(ctx context.Context) (func(), error) {
	cleanup := parser.CleanupTempConfigsDirectory

	if err := parser.ResolveConfigsDirectory(ctx); err != nil {
		return cleanup, fmt.Errorf("resolving config source: %w", err)
	}

	exists, err := parser.ConfigFilesExist(globals.ConfigsDirectory)
	if err != nil {
		return cleanup, fmt.Errorf("checking config files: %w", err)
	}
	if !exists {
		return cleanup, notFoundError(globals.ConfigsDirectory)
	}

	parser.ParseConfigFiles(ctx, globals.ConfigsDirectory)
	return cleanup, nil
}

// notFoundError names the clusters that DO have a config on disk.
//
// Listed rather than offered as a picker: every caller of Prepare goes on to
// create, mutate or destroy real cloud infrastructure, and an arrow-key
// selection is an easy way to act on the wrong cluster. Naming it is cheap;
// the operator still has to type which one they meant.
func notFoundError(configsDirectory string) error {
	available := clusterdir.List()
	if len(available) == 0 {
		return fmt.Errorf(
			"config files not found under %q — run `kubeaid-cli config generate` first to create them",
			configsDirectory,
		)
	}

	return fmt.Errorf(
		"config files not found under %q\n\nclusters with a saved config:\n  %s\n\n"+
			"re-run with --%s <name>, or --%s <path>",
		configsDirectory,
		strings.Join(available, "\n  "),
		constants.FlagNameClusterName,
		constants.FlagNameConfigsDirectory,
	)
}
