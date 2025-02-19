package upgrade

import (
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud/aws"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/core"
	"github.com/spf13/cobra"
)

var AWSCmd = &cobra.Command{
	Use:   "aws",
	Short: "Bootstrap a self-managed Kubernetes cluster in AWS",
	Run: func(cmd *cobra.Command, args []string) {
		core.UpgradeCluster(cmd.Context(), core.UpgradeClusterArgs{
			NewKubernetesVersion: kubernetesVersion,

			CloudSpecificUpdates: aws.MachineTemplateUpdates{
				AMIID: newAMIID,
			},
		})
	},
}

var newAMIID string

func init() {
	// Flags.

	config.RegisterAWSCredentialsFlags(AWSCmd)

	AWSCmd.PersistentFlags().
		StringVar(&newAMIID, constants.FlagNameAMIID, "", "ID of the AMI which supports the new Kubernetes version")
}
