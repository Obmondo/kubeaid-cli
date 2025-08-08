package generatecli

import (
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/docker"
	"github.com/spf13/cobra"
)

var BareMetalCmd = &cobra.Command{
	Use: "bare-metal",

	Short: "Generate a sample KubeAid Bootstrap Script config file, for deploying a Linux bare-metal based cluster",

	Run: func(cmd *cobra.Command, args []string) {
		dockerCommand.Cmd = append(dockerCommand.Cmd, " bare-metal")
		addRequiredFlagsToCommand()
		docker.ExecuteDockerCommand(dockerCommand)
	},
}
