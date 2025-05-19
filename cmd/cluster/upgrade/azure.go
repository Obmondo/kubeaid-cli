package upgrade

import (
	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud/azure"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/core"
)

var AzureCmd = &cobra.Command{
	Use: "azure",

	Short: "Trigger Kubernetes version upgrade for the provisioned Azure based cluster",

	Run: func(cmd *cobra.Command, args []string) {
		core.UpgradeCluster(cmd.Context(), skipPRFlow, core.UpgradeClusterArgs{
			NewKubernetesVersion: kubernetesVersion,

			CloudSpecificUpdates: azure.AzureMachineTemplateUpdates{
				ImageID: newImageID,
			},
		})
	},
}

var newImageID string

func init() {
	// Flags.

	AzureCmd.PersistentFlags().
		StringVar(&newImageID, constants.FlagNameImageID, "", "ID of the image which supports the new Kubernetes version")
}
