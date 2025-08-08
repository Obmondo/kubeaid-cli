package generatecli

import (
	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/docker"
)

var LocalCmd = &cobra.Command{
	Use: "local",

	Short: "Generate a sample KubeAid Bootstrap Script config file, for deploying a local K3D based cluster (for testing purposes)",

	Run: func(cmd *cobra.Command, args []string) {
		dockerCommand.Cmd = append(dockerCommand.Cmd, "local")
		addRequiredFlagsToCommand()
		docker.ExecuteDockerCommand(dockerCommand)
	},
}
