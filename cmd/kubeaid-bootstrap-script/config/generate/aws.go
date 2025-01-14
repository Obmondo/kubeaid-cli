package generate

import (
	"context"

	"github.com/Obmondo/kubeaid-bootstrap-script/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/constants"
	"github.com/spf13/cobra"
)

var AWSCmd = &cobra.Command{
	Use: "aws",
	Run: func(cmd *cobra.Command, args []string) {
		config.GenerateSampleConfig(context.Background(), constants.CloudProviderAWS)
	},
}
