package hetzner

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
)

var HCloudCmd = &cobra.Command{
	Use: "hcloud",

	Short: "Generate a sample KubeAid Bootstrap Script config file for Hetzner (using only HCloud)",

	Run: func(cmd *cobra.Command, args []string) {
		config.GenerateSampleConfig(cmd.Context(), &config.GenerateSampleConfigArgs{
			CloudProvider: constants.CloudProviderHetzner,
			HetznerMode:   aws.String(constants.HetznerModeHCloud),
		})
	},
}
