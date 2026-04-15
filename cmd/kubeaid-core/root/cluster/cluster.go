// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package cluster

import (
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-bootstrap-script/cmd/kubeaid-core/root/cluster/delete"
	"github.com/Obmondo/kubeaid-bootstrap-script/cmd/kubeaid-core/root/cluster/upgrade"
	configSetup "github.com/Obmondo/kubeaid-bootstrap-script/pkg/config/setup"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils"
)

var ClusterCmd = &cobra.Command{
	Use:   "cluster",
	Short: "Manage lifecycle of the KubeAid managed cluster",

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
	ClusterCmd.AddCommand(BootstrapCmd)
	ClusterCmd.AddCommand(TestCmd)
	ClusterCmd.AddCommand(upgrade.UpgradeCmd)
	ClusterCmd.AddCommand(delete.DeleteCmd)
	ClusterCmd.AddCommand(RecoverCmd)

	// Flags.

	ClusterCmd.PersistentFlags().
		BoolVar(&skipPRWorkflow, constants.FlagNameSkipPRWorkflow, false,
			"Skip the PR workflow and let KubeAid Bootstrap Script push changes directly to the default branch")

	ClusterCmd.PersistentFlags().
		StringVar(&managementClusterName,
			constants.FlagNameManagementClusterName,
			constants.FlagNameManagementClusterNameDefaultValue,
			"Name of the local K3D management cluster",
		)
}
