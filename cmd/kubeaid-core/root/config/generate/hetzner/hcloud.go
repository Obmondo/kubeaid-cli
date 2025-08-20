// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package hetzner

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	gitUtils "github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/git"
)

var HCloudCmd = &cobra.Command{
	Use: "hcloud",

	Short: "Generate a sample KubeAid Bootstrap Script config file for Hetzner (using only HCloud)",

	Run: func(cmd *cobra.Command, args []string) {
		ctx := cmd.Context()
		config.GenerateSampleConfig(ctx, &config.GenerateSampleConfigArgs{
			CloudProvider: constants.CloudProviderHetzner,
			HetznerMode:   aws.String(constants.HetznerModeHCloud),
		}, gitUtils.GetLatestTagFromObmondoKubeAid(ctx))
	},
}
