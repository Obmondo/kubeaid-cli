// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package upgrade

import (
	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-cli/pkg/cloud/aws"
	"github.com/Obmondo/kubeaid-cli/pkg/cloud/azure"
	"github.com/Obmondo/kubeaid-cli/pkg/cloud/hetzner"
	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/core"
	"github.com/Obmondo/kubeaid-cli/pkg/globals"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/assert"
)

var UpgradeCmd = &cobra.Command{
	Use: "upgrade",

	Short: "Upgrade a KubeAid managed K8s cluster (to the K8s version and machine images in general.yaml)",

	// Reject legacy 'cluster upgrade <provider>' invocations loudly, instead of silently
	// running an upgrade with the stray argument ignored.
	Args: cobra.NoArgs,

	// GitOps driven : no flags. The cloud provider gets auto-detected from general.yaml, the
	// target Kubernetes version is cluster.k8sVersion, and machine images come from the
	// provider's own config section.
	Run: func(cmd *cobra.Command, args []string) {
		targetK8sVersion := config.ParsedGeneralConfig.Cluster.K8sVersion

		switch globals.CloudProviderName {
		case constants.CloudProviderBareMetal:
			core.UpgradeClusterUsingKubeOne(cmd.Context(), core.UpgradeKubeOneClusterArgs{
				SkipPRWorkflow: skipPRWorkflow,
			})

		case constants.CloudProviderAWS:
			core.UpgradeCluster(cmd.Context(), core.UpgradeClusterArgs{
				SkipPRWorkflow: skipPRWorkflow,

				NewKubernetesVersion: targetK8sVersion,

				// NOTE : The upgrade machinery applies a single AMI to the control-plane and
				//        every node-group - the control-plane one from general.yaml.
				CloudSpecificUpdates: aws.AWSMachineTemplateUpdates{
					AMIID: config.ParsedGeneralConfig.Cloud.AWS.ControlPlane.AMI.ID,
				},
			})

		case constants.CloudProviderAzure:
			core.UpgradeCluster(cmd.Context(), core.UpgradeClusterArgs{
				SkipPRWorkflow: skipPRWorkflow,

				NewKubernetesVersion: targetK8sVersion,

				CloudSpecificUpdates: azure.AzureMachineTemplateUpdates{
					NewImageOffer: config.ParsedGeneralConfig.Cloud.Azure.CanonicalUbuntuImage.Offer,
				},
			})

		case constants.CloudProviderHetzner:
			hetznerConfig := config.ParsedGeneralConfig.Cloud.Hetzner

			cloudSpecificUpdates := hetzner.HetznerMachineTemplateUpdates{}
			if hetznerConfig.HCloud != nil {
				cloudSpecificUpdates.NewImageName = hetznerConfig.HCloud.ImageName
			}
			if hetznerConfig.BareMetal != nil {
				cloudSpecificUpdates.NewImagePath = hetznerConfig.BareMetal.InstallImage.ImagePath
			}

			core.UpgradeCluster(cmd.Context(), core.UpgradeClusterArgs{
				SkipPRWorkflow: skipPRWorkflow,

				NewKubernetesVersion: targetK8sVersion,

				CloudSpecificUpdates: cloudSpecificUpdates,
			})

		default:
			assert.Assert(cmd.Context(), false,
				"Cluster upgrade isn't supported for the local provider. Recreate the dev environment instead",
			)
		}
	},
}

var skipPRWorkflow bool

func init() {
	// Flags.

	UpgradeCmd.PersistentFlags().
		BoolVar(&skipPRWorkflow, constants.FlagNameSkipPRWorkflow, false,
			"Skip the PR workflow and let KubeAid Bootstrap Script push changes directly to the default branch",
		)
}
