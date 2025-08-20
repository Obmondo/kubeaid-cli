// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package devenv

import (
	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/core"
)

var CreateCmd = &cobra.Command{
	Use: "create",

	Short: "Create and setup the local K3D management cluster, for deploying the KubeAid managed main cluster",

	Run: func(cmd *cobra.Command, args []string) {
		core.CreateDevEnv(cmd.Context(), &core.CreateDevEnvArgs{
			ManagementClusterName:    managementClusterName,
			SkipMonitoringSetup:      skipMonitoringSetup,
			SkipPRWorkflow:           skipPRWorkflow,
			IsPartOfDisasterRecovery: false,
		})
	},
}

var skipMonitoringSetup,
	skipPRWorkflow bool

func init() {
	// Flags.

	CreateCmd.Flags().
		BoolVar(&skipMonitoringSetup, constants.FlagNameSkipMonitoringSetup, false,
			"Skip KubePrometheus installation",
		)

	CreateCmd.Flags().
		BoolVar(&skipPRWorkflow, constants.FlagNameSkipPRWorkflow, false,
			"Skip the PR workflow and let KubeAid Bootstrap Script push changes directly to the default branch",
		)
}
