package upgradecli

import (
	"fmt"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/docker"
	"github.com/spf13/cobra"
)

var AWSCmd = &cobra.Command{
	Use: "aws",

	Short: "Trigger Kubernetes version upgrade for the provisioned AWS based cluster",

	Run: func(cmd *cobra.Command, args []string) {
		dockerCommand.Cmd = append(dockerCommand.Cmd, "aws")

		addRequiredFlagsToCommand()

		if newAMIID != "" {
			dockerCommand.Cmd = append(dockerCommand.Cmd, fmt.Sprintf("--%s", constants.FlagNameAMIID))
			dockerCommand.Cmd = append(dockerCommand.Cmd, newAMIID)
		}
		docker.ExecuteDockerCommand(dockerCommand)
	},
}

var newAMIID string

func init() {
	// Flags.
	AWSCmd.PersistentFlags().
		StringVar(&newAMIID, constants.FlagNameAMIID, "", "ID of the AMI which supports the new Kubernetes version")
}
