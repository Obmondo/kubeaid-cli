package hetznercli

import (
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/docker"
	"github.com/spf13/cobra"
)

var HCloudCmd = &cobra.Command{
	Use: "hcloud",

	Short: "Generate a sample KubeAid Bootstrap Script config file for Hetzner (using only HCloud)",

	Run: func(cmd *cobra.Command, args []string) {
		dockerCommand.Cmd = append(dockerCommand.Cmd, "hcloud")
		addRequiredFlagsToCommand()
		docker.ExecuteDockerCommand(dockerCommand)
	},
}
