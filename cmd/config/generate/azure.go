package generate

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
)

var AzureCmd = &cobra.Command{
	Use: "azure",

	Short: "Generate a sample KubeAid Bootstrap Script config file, for deploying an Azure based cluster",

	Run: func(cmd *cobra.Command, args []string) {
		config.GenerateSampleConfig(context.Background(), &config.GenerateSampleConfigArgs{
			CloudProvider: constants.CloudProviderAzure,
		})
	},
}
