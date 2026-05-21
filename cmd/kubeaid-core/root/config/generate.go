// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package config

import (
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path"

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

		// Refuse to clobber an existing complete config pair unless
		// the prompt can resume an interrupted or incomplete run.
		// Partial state (dir exists, only one of the two files) falls
		// through to the prompt as before.
		generalPath := path.Join(globals.ConfigsDirectory, "general.yaml")
		secretsPath := path.Join(globals.ConfigsDirectory, "secrets.yaml")
		if fileExists(generalPath) && fileExists(secretsPath) {
			needsResume, err := prompt.ConfigNeedsInteractiveResume(globals.ConfigsDirectory)
			assert.AssertErrNil(ctx, err, "Failed checking existing config prompt state")
			if !needsResume {
				slog.ErrorContext(ctx, "Config files already exist — refusing to overwrite",
					slog.String("general", generalPath),
					slog.String("secrets", secretsPath),
				)
				fmt.Fprintln(os.Stderr,
					"\nDelete the files (or pass a different --configs-directory) to re-run the prompt.")
				os.Exit(1)
			}
		}

		if err := os.MkdirAll(globals.ConfigsDirectory, 0o750); err != nil {
			assert.AssertErrNil(ctx, err,
				"Failed creating configs directory",
				slog.String("path", globals.ConfigsDirectory),
			)
		}

		if err := prompt.ConfigFromPrompt(globals.ConfigsDirectory); err != nil {
			assert.AssertErrNil(ctx, err, "Interactive config generation failed")
		}
	},
}

// fileExists is the small "is this a regular file" check the
// idempotency guard needs. Anything else (dir, symlink with broken
// target) counts as "not present" so the prompt can write through.
func fileExists(p string) bool {
	info, err := os.Stat(p)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false
		}
		// Permission or unrelated error — treat as present so the
		// guard fails closed; the operator gets the same "delete or
		// pass --configs-directory" message and can investigate.
		return true
	}
	return info.Mode().IsRegular()
}
