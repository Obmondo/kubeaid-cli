// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package generate

import (
	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	gitUtils "github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/git"
)

var AWSCmd = &cobra.Command{
	Use: "aws",

	Short: "Generate a sample KubeAid Bootstrap Script config file, for deploying an AWS based cluster",

	Run: func(cmd *cobra.Command, args []string) {
		ctx := cmd.Context()
		config.GenerateSampleConfig(ctx, &config.GenerateSampleConfigArgs{
			CloudProvider: constants.CloudProviderAWS,
		}, gitUtils.GetLatestTagFromObmondoKubeAid(ctx))
	},
}
