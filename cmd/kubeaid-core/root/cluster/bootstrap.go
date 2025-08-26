// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package cluster

import (
	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/core"
)

var BootstrapCmd = &cobra.Command{
	Use: "bootstrap",

	Short: "Bootstrap a KubeAid managed cluster",

	Run: func(cmd *cobra.Command, args []string) {
		core.BootstrapCluster(cmd.Context(), core.BootstrapClusterArgs{
			CreateDevEnvArgs: &core.CreateDevEnvArgs{
				ManagementClusterName:    managementClusterName,
				SkipMonitoringSetup:      skipMonitoringSetup,
				SkipPRWorkflow:           skipPRWorkflow,
				IsPartOfDisasterRecovery: false,
			},
			SkipClusterctlMove: skipClusterctlMove,
		})
	},
}

var skipMonitoringSetup,
	skipClusterctlMove bool

func init() {
	// Flags.

	BootstrapCmd.PersistentFlags().
		BoolVar(&skipMonitoringSetup, constants.FlagNameSkipMonitoringSetup, false,
			"Skip KubePrometheus installation",
		)

	BootstrapCmd.PersistentFlags().
		BoolVar(&skipClusterctlMove, constants.FlagNameSkipClusterctlMove, false,
			"Skip executing the 'clusterctl move' command",
		)
}
