package deletecli

import (
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/docker"
	"github.com/spf13/cobra"
)

var MainCmd = &cobra.Command{
	Use: "main",

	Short: "Delete a KubeAid managed cluster",

	Run: func(cmd *cobra.Command, args []string) {
		dockerCommand.Cmd = append(dockerCommand.Cmd, "main")
		addRequiredFlagsToCommand()
		docker.ExecuteDockerCommand(dockerCommand)
	},
}
