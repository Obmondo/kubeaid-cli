package generate

import (
	"context"

	"github.com/Obmondo/kubeaid-bootstrap-script/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/constants"
	"github.com/spf13/cobra"
)

var HetznerCmd = &cobra.Command{
	Use: "hetzner",
	Run: func(cmd *cobra.Command, args []string) {
		config.GenerateSampleConfig(context.Background(), constants.CloudProviderHetzner)
	},
}
