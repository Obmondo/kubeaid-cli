// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package upgrade

import (
	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud/hetzner"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/core"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
)

var HetznerCmd = &cobra.Command{
	Use: "hetzner",

	Short: "Trigger Kubernetes version and / or OS upgrade for a KubeAid managed Hetzner based cluster",

	Run: func(cmd *cobra.Command, args []string) {
		assert.Assert(cmd.Context(),
			(len(newKubernetesVersion) > 0) || ((len(newImageName) > 0) || (len(newImagePath) > 0)),
			"No upgrade details provided",
		)

		core.UpgradeCluster(cmd.Context(), core.UpgradeClusterArgs{
			SkipPRWorkflow: skipPRWorkflow,

			NewKubernetesVersion: newKubernetesVersion,

			CloudSpecificUpdates: hetzner.HetznerMachineTemplateUpdates{
				HCloudMachineTemplateUpdates: hetzner.HCloudMachineTemplateUpdates{
					NewImageName: newImageName,
				},
				HetznerBareMetalMachineTemplateUpdates: hetzner.HetznerBareMetalMachineTemplateUpdates{
					NewImagePath: newImagePath,
				},
			},
		})
	},
}

var newImageName, newImagePath string

func init() {
	// Flags.

	HetznerCmd.Flags().
		StringVar(&newImageName, constants.FlagNameNewImageName, "",
			"New HCloud image name",
		)

	HetznerCmd.Flags().
		StringVar(&newImagePath, constants.FlagNameNewImagePath, "",
			"New Hetzner Bare Metal install-image script path",
		)
}
