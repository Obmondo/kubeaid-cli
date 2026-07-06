// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package sync

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/core"
	"github.com/Obmondo/kubeaid-cli/pkg/globals"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/assert"
)

var SyncCmd = &cobra.Command{
	Use: "sync",

	Short: "Converge a KubeAid managed K8s cluster onto general.yaml, without a Kubernetes version change",

	Args: cobra.NoArgs,

	// GitOps driven : no flags. Desired state (kubelet tuning, helm releases, addons, hosts)
	// is read from general.yaml; version changes are 'cluster upgrade's job. Disruptive
	// reconciles (kubelet flags need a rolling per-node procedure) ask for consent first.
	Run: func(cmd *cobra.Command, args []string) {
		switch globals.CloudProviderName {
		case constants.CloudProviderBareMetal:
			core.SyncClusterUsingKubeOne(cmd.Context(), core.SyncKubeOneClusterArgs{
				SkipPRWorkflow: skipPRWorkflow,
			})

		default:
			assert.Assert(cmd.Context(), false, fmt.Sprintf(
				"'cluster sync' is only needed for the Bare Metal (KubeOne) provider - on %s, merged kubeaid-config changes get reconciled by ArgoCD",
				globals.CloudProviderName,
			))
		}
	},
}

var skipPRWorkflow bool

func init() {
	// Flags.

	SyncCmd.PersistentFlags().
		BoolVar(
			&skipPRWorkflow, constants.FlagNameSkipPRWorkflow, false,
			"Skip the PR workflow and let KubeAid Bootstrap Script push changes directly to the default branch",
		)
}
