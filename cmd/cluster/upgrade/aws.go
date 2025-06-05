package upgrade

import (
	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud/aws"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/core"
)

var AWSCmd = &cobra.Command{
	Use: "aws",

	Short: "Trigger Kubernetes version upgrade for the provisioned AWS based cluster",

	Run: func(cmd *cobra.Command, args []string) {
		core.UpgradeCluster(cmd.Context(), skipPRWorkflow, core.UpgradeClusterArgs{
			NewKubernetesVersion: kubernetesVersion,

			CloudSpecificUpdates: aws.AWSMachineTemplateUpdates{
				AMIID: newAMIID,
			},
		})
	},
}

var newAMIID string

func init() {
	// Flags.

	AWSCmd.PersistentFlags().
		StringVar(&newAMIID, constants.FlagNameAMIID, "", "ID of the AMI which supports the new Kubernetes version")
}
