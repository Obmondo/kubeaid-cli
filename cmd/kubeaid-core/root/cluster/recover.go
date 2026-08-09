// Copyright 2025 Obmondo
// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/core"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/assert"
)

var RecoverCmd = &cobra.Command{
	Use: "recover",

	Short: "Recover a KubeAid managed K8s cluster (from a disaster recovery backup)",

	Run: func(cmd *cobra.Command, args []string) {
		// EKS recovery isn't wired yet — recover re-runs the bootstrap and the
		// Velero restore, and neither has been exercised against a managed
		// control plane. Fail loudly instead of half-recovering.
		assert.Assert(cmd.Context(), !config.EKSEnabled(),
			"`cluster recover` doesn't support EKS clusters yet — re-bootstrap and restore from the Velero backup manually",
		)

		core.RecoverCluster(cmd.Context(),
			managementClusterName,
			skipPRWorkflow,
		)
	},
}

var skipPRWorkflow bool

func init() {
	// Flags

	RecoverCmd.PersistentFlags().
		BoolVar(&skipPRWorkflow, constants.FlagNameSkipPRWorkflow, false,
			"Skip the PR workflow and let KubeAid Bootstrap Script push changes directly to the default branch",
		)
}
