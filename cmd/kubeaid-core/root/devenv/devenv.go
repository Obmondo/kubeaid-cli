// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package devenv

import (
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	configSetup "github.com/Obmondo/kubeaid-bootstrap-script/pkg/config/setup"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils"
)

var DevenvCmd = &cobra.Command{
	Use:   "devenv",
	Short: "Manage local development environment (i.e. the management cluster)",

	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		ctx := cmd.Context()

		cleanup, err := configSetup.Prepare(ctx)
		if err != nil {
			slog.ErrorContext(ctx, "Failed preparing config files",
				slog.String("error", err.Error()),
			)
			cleanup()
			os.Exit(1)
		}
		cobra.OnFinalize(cleanup)

		// Initialize temp directory.
		utils.InitTempDir(ctx)

		// Ensure required runtime dependencies are installed.
		utils.EnsureRuntimeDependenciesInstalled(ctx)
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
			constants.FlagNameManagementClusterNameDefaultValue,
			"Name of the local K3D management cluster",
		)
}
