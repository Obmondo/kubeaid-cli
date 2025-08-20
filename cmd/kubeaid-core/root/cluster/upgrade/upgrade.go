// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package upgrade

import (
	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
)

var UpgradeCmd = &cobra.Command{
	Use: "upgrade",
}

var (
	newKubernetesVersion string
	skipPRWorkflow       bool
)

func init() {
	// Subcommands.
	UpgradeCmd.AddCommand(AWSCmd)
	UpgradeCmd.AddCommand(AzureCmd)

	// Flags.

	UpgradeCmd.PersistentFlags().
		StringVar(&newKubernetesVersion, constants.FlagNameNewK8sVersion, "", "New Kubernetes version, the cluster will be upgraded to")

	UpgradeCmd.PersistentFlags().
		BoolVar(&skipPRWorkflow, constants.FlagNameSkipPRWorkflow, false,
			"Skip the PR workflow and let KubeAid Bootstrap Script push changes directly to the default branch",
		)
}
