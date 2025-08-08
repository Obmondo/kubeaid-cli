package clustercli

import (
	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/docker"
)

var TestCmd = &cobra.Command{
	Use: "test",

	Short: "Test whether KubeAid Bootstrap Script properly bootstrapped your cluster or not",

	Run: func(cmd *cobra.Command, args []string) {
		dockerCommand.Cmd = append(dockerCommand.Cmd, "test")

		addRequiredFlagsToCommand()

		docker.ExecuteDockerCommand(dockerCommand)
	},
}
