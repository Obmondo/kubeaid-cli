// Copyright 2025 Obmondo
// SPDX-License-Identifier: Apache-2.0

package devenv

import (
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	configSetup "github.com/Obmondo/kubeaid-cli/pkg/config/setup"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/utils"
)

var DevenvCmd = &cobra.Command{
	Use:   "devenv",
	Short: "Manage the local development environment (i.e. the K3D management cluster)",

	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		ctx := cmd.Context()

		if err := configSetup.Prepare(ctx); err != nil {
			slog.ErrorContext(ctx, "Failed preparing config files",
				slog.String("error", err.Error()),
			)
			os.Exit(1)
		}

		// Initialize temp directory.
		if err := utils.InitTempDir(ctx); err != nil {
			slog.ErrorContext(ctx, "Failed initializing temp dir", slog.String("error", err.Error()))
			os.Exit(1)
		}
	},
}

var managementClusterName string

func init() {
	// Subcommands.
	DevenvCmd.AddCommand(CreateCmd)

	// Flags.

	DevenvCmd.PersistentFlags().
		StringVar(&managementClusterName,
			constants.FlagNameManagementClusterName,
			"",
			"Name of the local K3D management cluster. When omitted, defaults to "+
				constants.ManagementClusterNamePrefix+"<cluster-name> (from general.yaml)",
		)
}
