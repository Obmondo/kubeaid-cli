package upgradecli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/docker"
)

var AzureCmd = &cobra.Command{
	Use: "azure",

	Short: "Trigger Kubernetes version and / or OS upgrade for a KubeAid managed Azure based cluster",

	Run: func(cmd *cobra.Command, args []string) {
		dockerCommand.Cmd = append(dockerCommand.Cmd, "azure")

		addRequiredFlagsToCommand()

		if newImageOffer != "" {
			dockerCommand.Cmd = append(dockerCommand.Cmd, fmt.Sprintf("--%s", constants.FlagNameNewImageOffer))
			dockerCommand.Cmd = append(dockerCommand.Cmd, newImageOffer)
		}

		docker.ExecuteDockerCommand(dockerCommand)
	},
}

var newImageOffer string

func init() {
	// Flags.
	AzureCmd.Flags().
		StringVar(&newImageOffer, constants.FlagNameNewImageOffer, "", "New Canonical Ubuntu image offer to use for the OS upgrade")

}
