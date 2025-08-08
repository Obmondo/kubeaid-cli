package clustercli

import (
	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/docker"
)

var RecoverCmd = &cobra.Command{
	Use: "recover",

	Short: "Recover a KubeAid managed Kubernetes cluster",

	Run: func(cmd *cobra.Command, args []string) {
		dockerCommand.Cmd = append(dockerCommand.Cmd, "recover")

		addRequiredFlagsToCommand()

		docker.ExecuteDockerCommand(dockerCommand)
	},
}

var (
	skipPRWorkflow bool
)

func init() {
	// Flags
	RecoverCmd.PersistentFlags().
		BoolVar(&skipPRWorkflow, constants.FlagNameSkipPRWorkflow, false,
			"Skip the PR workflow and let KubeAid Bootstrap Script push changes directly to the default branch",
		)

}
