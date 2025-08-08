package hetznercli

import (
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/docker"
	"github.com/spf13/cobra"
)

var HybridCmd = &cobra.Command{
	Use: "hybrid",

	Short: "Generate a sample KubeAid Bootstrap Script config file for Hetzner (having control-plane on HCloud and node-groups on HCloud / Hetzner bare-metal)",

	Run: func(cmd *cobra.Command, args []string) {
		dockerCommand.Cmd = append(dockerCommand.Cmd, "hybrid")
		addRequiredFlagsToCommand()
		docker.ExecuteDockerCommand(dockerCommand)
	},
}
