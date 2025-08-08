package generatecli

import (
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/docker"
	"github.com/spf13/cobra"
)

var AWSCmd = &cobra.Command{
	Use: "aws",

	Short: "Generate a sample KubeAid Bootstrap Script config file, for deploying an AWS based cluster",

	Run: func(cmd *cobra.Command, args []string) {
		dockerCommand.Cmd = append(dockerCommand.Cmd, "aws")
		addRequiredFlagsToCommand()
		docker.ExecuteDockerCommand(dockerCommand)
	},
}
