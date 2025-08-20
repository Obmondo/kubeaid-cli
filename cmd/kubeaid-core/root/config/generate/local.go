// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package generate

import (
	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	gitUtils "github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/git"
)

var LocalCmd = &cobra.Command{
	Use: "local",

	Short: "Generate a sample KubeAid Bootstrap Script config file, for deploying a local K3D based cluster (for testing purposes)",

	Run: func(cmd *cobra.Command, args []string) {
		ctx := cmd.Context()
		config.GenerateSampleConfig(ctx, &config.GenerateSampleConfigArgs{
			CloudProvider: constants.CloudProviderLocal,
		}, gitUtils.GetLatestTagFromObmondoKubeAid(ctx))
	},
}
