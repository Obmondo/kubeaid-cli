package upgrade

import (
	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud/azure"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/core"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
)

var AzureCmd = &cobra.Command{
	Use: "azure",

	Short: "Trigger Kubernetes version and / or OS upgrade for a KubeAid managed Azure based cluster",

	Run: func(cmd *cobra.Command, args []string) {
		assert.Assert(cmd.Context(),
			(len(newKubernetesVersion) > 0) || (len(newImageOffer) > 0),
			"No upgrade details provided",
		)

		core.UpgradeCluster(cmd.Context(), core.UpgradeClusterArgs{
			SkipPRWorkflow: skipPRWorkflow,

			NewKubernetesVersion: newKubernetesVersion,

			CloudSpecificUpdates: azure.AzureMachineTemplateUpdates{
				NewImageOffer: newImageOffer,
			},
		})
	},
}

var newImageOffer string

func init() {
	// Flags.

	AzureCmd.Flags().
		StringVar(&newImageOffer, constants.FlagNameNewImageOffer, "", "New Canonical Ubuntu image offer to use for the OS upgrade")
}
