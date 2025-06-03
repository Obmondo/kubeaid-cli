package hetzner

import (
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/spf13/cobra"
)

var BareMetalCmd = &cobra.Command{
	Use: "bare-metal",

	Short: "Generate a sample KubeAid Bootstrap Script config file for Hetzner (using only bare-metal)",

	Run: func(cmd *cobra.Command, args []string) {
		config.GenerateSampleConfig(cmd.Context(), &config.GenerateSampleConfigArgs{
			CloudProvider: constants.CloudProviderHetzner,
			HetznerMode:   aws.String(constants.HetznerModeBareMetal),
		})
	},
}
