package generate

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
)

// HetznerCmd represents the base command for Hetzner options
var HetznerCmd = &cobra.Command{
	Use:   "hetzner",
	Short: "Generate a sample KubeAid Bootstrap Script config file for Hetzner",
}

// HetznerRobotCmd represents the baremetal-robot subcommand
var HetznerRobotCmd = &cobra.Command{
	Use:   "robot",
	Short: "Generate a sample KubeAid Bootstrap Script config file for Hetzner bare metal robot",
	Run: func(cmd *cobra.Command, args []string) {
		config.GenerateSampleConfig(
			context.Background(),
			constants.CloudProviderHetzner,
			constants.HetznerModeBareMetal,
		)
	},
}

// HetznerCloudCmd represents the hcloud subcommand
var HetznerHCloudCmd = &cobra.Command{
	Use:   "hcloud",
	Short: "Generate a sample KubeAid Bootstrap Script config file for Hetzner Cloud",
	Run: func(cmd *cobra.Command, args []string) {
		config.GenerateSampleConfig(
			context.Background(),
			constants.CloudProviderHetzner,
			constants.HetznerModeHCloud,
		)
	},
}

func init() {
	HetznerCmd.AddCommand(HetznerRobotCmd)
	HetznerCmd.AddCommand(HetznerHCloudCmd)
}
