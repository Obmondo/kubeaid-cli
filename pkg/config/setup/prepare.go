// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package setup

import (
	"context"
	"fmt"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config/parser"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
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
		return cleanup, fmt.Errorf(
			"config files not found under %q — run `kubeaid-cli config generate` first to create them",
			globals.ConfigsDirectory,
		)
	}

	parser.ParseConfigFiles(ctx, globals.ConfigsDirectory)
	return cleanup, nil
}
