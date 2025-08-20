// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package cluster

import (
	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-bootstrap-script/cmd/kubeaid-core/root/cluster/delete"
	"github.com/Obmondo/kubeaid-bootstrap-script/cmd/kubeaid-core/root/cluster/upgrade"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config/parser"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils"
)

var ClusterCmd = &cobra.Command{
	Use: "cluster",

	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// Parse config files.
		parser.ParseConfigFiles(cmd.Context(), globals.ConfigsDirectory)

		// Initialize temp directory.
		utils.InitTempDir(cmd.Context())

		// Ensure required runtime dependencies are installed.
		utils.EnsureRuntimeDependenciesInstalled(cmd.Context())
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
			"Skip the PR workflow and let KubeAid Bootstrap Script push changes directly to the default branch",
		)

	ClusterCmd.PersistentFlags().
		StringVar(&managementClusterName, constants.FlagNameManagementClusterName, constants.FlagNameManagementClusterNameDefaultValue,
			"Name of the local K3D management cluster",
		)
}
