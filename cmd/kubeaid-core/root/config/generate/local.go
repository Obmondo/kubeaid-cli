// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package generate

import (
	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
)

var LocalCmd = &cobra.Command{
	Use: "local",

	Short: "Generate a sample KubeAid Bootstrap Script config file, for deploying a local K3D based cluster (for testing purposes)",

	Run: func(cmd *cobra.Command, args []string) {
		config.GenerateSampleConfig(cmd.Context(), &config.GenerateSampleConfigArgs{
			CloudProvider: constants.CloudProviderLocal,
		})
	},
}
