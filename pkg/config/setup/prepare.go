// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package setup

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config/parser"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config/prompt"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
)

// Prepare resolves the config source, optionally generates config files via interactive prompt,
// and parses config files. It returns a cleanup function that should run at process finalization
// to remove temp config dirs created for stdin-based configs.
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
		slog.InfoContext(ctx, "Config files not found, starting interactive setup",
			slog.String("path", globals.ConfigsDirectory),
		)

		if err := prompt.ConfigFromPrompt(globals.ConfigsDirectory); err != nil {
			return cleanup, fmt.Errorf("interactive config setup failed: %w", err)
		}
	}

	parser.ParseConfigFiles(ctx, globals.ConfigsDirectory)

	return cleanup, nil
}
