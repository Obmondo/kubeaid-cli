package deletecli

import (
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/docker"
	"github.com/spf13/cobra"
)

var ManagementCmd = &cobra.Command{
	Use: "management",

	Short: "Delete the K3D based local management cluster (used to bootstrap your KubeAid managed main cluster)",

	Run: func(cmd *cobra.Command, args []string) {
		dockerCommand.Cmd = append(dockerCommand.Cmd, "management")
		addRequiredFlagsToCommand()
		docker.ExecuteDockerCommand(dockerCommand)
	},
}
