package generatecli

import (
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/docker"
	"github.com/spf13/cobra"
)

var AzureCmd = &cobra.Command{
	Use: "azure",

	Short: "Generate a sample KubeAid Bootstrap Script config file, for deploying an Azure based cluster",

	Run: func(cmd *cobra.Command, args []string) {
		dockerCommand.Cmd = append(dockerCommand.Cmd, "azure")
		addRequiredFlagsToCommand()
		docker.ExecuteDockerCommand(dockerCommand)
	},
}
