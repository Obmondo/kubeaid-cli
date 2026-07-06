// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package cluster

import (
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-cli/cmd/kubeaid-core/root/cluster/delete"
	clusterSync "github.com/Obmondo/kubeaid-cli/cmd/kubeaid-core/root/cluster/sync"
	"github.com/Obmondo/kubeaid-cli/cmd/kubeaid-core/root/cluster/upgrade"
	configSetup "github.com/Obmondo/kubeaid-cli/pkg/config/setup"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/utils"
)

var ClusterCmd = &cobra.Command{
	Use:   "cluster",
	Short: "Manage the lifecycle of a KubeAid managed K8s cluster",

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
		if err := utils.InitTempDir(ctx); err != nil {
			slog.ErrorContext(ctx, "Failed initializing temp dir", slog.String("error", err.Error()))
			os.Exit(1)
		}
	},
}

var managementClusterName string

func init() {
	// Subcommands.
	ClusterCmd.AddCommand(BootstrapCmd)
	ClusterCmd.AddCommand(TestCmd)
	ClusterCmd.AddCommand(upgrade.UpgradeCmd)
	ClusterCmd.AddCommand(clusterSync.SyncCmd)
	ClusterCmd.AddCommand(delete.DeleteCmd)
	ClusterCmd.AddCommand(RecoverCmd)

	// Flags.

	ClusterCmd.PersistentFlags().
		BoolVar(&skipPRWorkflow, constants.FlagNameSkipPRWorkflow, false,
			"Skip the PR workflow and let KubeAid Bootstrap Script push changes directly to the default branch")

	ClusterCmd.PersistentFlags().
		StringVar(&managementClusterName,
			constants.FlagNameManagementClusterName,
			"",
			"Name of the local K3D management cluster. When omitted, defaults to "+
				constants.ManagementClusterNamePrefix+"<cluster-name> (from general.yaml)",
		)
}
