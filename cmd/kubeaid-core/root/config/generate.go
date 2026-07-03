// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package config

import (
	_ "embed"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-cli/pkg/config/prompt"
	"github.com/Obmondo/kubeaid-cli/pkg/globals"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/assert"
)

// SampleConfigFileGeneral / SampleConfigFileSecrets stay embedded — they
// power tooling that still wants the hand-edit template shape (docs
// generation, integration tests that need a stable schema example).
// The `generate` command itself no longer writes them.
var (
	//go:embed templates/general.yaml
	SampleConfigFileGeneral string

	//go:embed templates/secrets.yaml
	SampleConfigFileSecrets string
)

var GenerateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Interactively generate general.yaml and secrets.yaml via the config prompt",
	Long: `Walks through an interactive prompt that collects all required values
(cluster basics, cloud-provider credentials, vSwitch / Hetzner Robot
hosts when applicable, Git / KubeAid fork URLs, etc.) and writes the
resulting general.yaml and secrets.yaml under --configs-directory.

cluster bootstrap consumes the files produced here; run config generate
first, review the output, then bootstrap.`,

	Run: func(cmd *cobra.Command, args []string) {
		ctx := cmd.Context()

		if err := os.MkdirAll(globals.ConfigsDirectory, 0o750); err != nil {
			assert.AssertErrNil(ctx, err,
				"Failed creating configs directory",
				slog.String("path", globals.ConfigsDirectory),
			)
		}

		// An existing config is not an error: the prompt offers to load it
		// as the pre-filled starting point (or start fresh), and only
		// rewrites the files after the final confirm.
		if err := prompt.ConfigFromPrompt(globals.ConfigsDirectory); err != nil {
			assert.AssertErrNil(ctx, err, "Interactive config generation failed")
		}
	},
}
