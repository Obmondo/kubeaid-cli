package hetzner

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	gitUtils "github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/git"
)

var HybridCmd = &cobra.Command{
	Use: "hybrid",

	Short: "Generate a sample KubeAid Bootstrap Script config file for Hetzner (having control-plane on HCloud and node-groups on HCloud / Hetzner bare-metal)",

	Run: func(cmd *cobra.Command, args []string) {
		ctx := cmd.Context()
		config.GenerateSampleConfig(ctx, &config.GenerateSampleConfigArgs{
			CloudProvider: constants.CloudProviderHetzner,
			HetznerMode:   aws.String(constants.HetznerModeHybrid),
		}, gitUtils.GetLatestTagFromObmondoKubeAid(ctx))
	},
}
