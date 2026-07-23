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

		// ArgoCD reconciles whatever is merged into kubeaid-config, but only `bootstrap`
		// ever re-renders values-capi-cluster.yaml from general.yaml. So a machineType or
		// control-plane replicas edit never reaches a provisioned ClusterAPI cluster on its
		// own : re-render it, and let ArgoCD carry it the rest of the way.
		case constants.CloudProviderHetzner:
			core.SyncCAPICluster(cmd.Context(), core.SyncCAPIClusterArgs{
				SkipPRWorkflow: skipPRWorkflow,
			})

		default:
			assert.Assert(cmd.Context(), false, fmt.Sprintf(
				"'cluster sync' does not support %s yet - re-run 'cluster bootstrap' to re-render values-capi-cluster.yaml",
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
