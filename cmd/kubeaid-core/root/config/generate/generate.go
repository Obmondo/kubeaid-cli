// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package generate

import (
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-bootstrap-script/cmd/kubeaid-core/root/config/generate/hetzner"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
)

var GenerateCmd = &cobra.Command{
	Use: "generate",

	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// Verify that config files directory doesn't already exist.
		_, err := os.Stat(globals.ConfigsDirectory)
		if err == nil {
			slog.ErrorContext(cmd.Context(),
				"Config files directory already exists",
				slog.String("path", globals.ConfigsDirectory),
			)
			os.Exit(1)
		}
	},
}

var KubeAidVersion string

func init() {
	// Subcommands.
	GenerateCmd.AddCommand(AWSCmd)
	GenerateCmd.AddCommand(AzureCmd)
	GenerateCmd.AddCommand(hetzner.HetznerCmd)
	GenerateCmd.AddCommand(LocalCmd)
	GenerateCmd.AddCommand(BareMetalCmd)
}
