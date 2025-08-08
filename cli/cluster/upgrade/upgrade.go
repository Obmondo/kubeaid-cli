package upgradecli

import (
	"fmt"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/docker"
	"github.com/spf13/cobra"
)

var UpgradeCmd = &cobra.Command{
	Use: "upgrade",

	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var (
	newKubernetesVersion string
	skipPRWorkflow       bool
	dockerCommand        docker.CommandOptions
)

func init() {
	dockerCommand.Cmd = []string{"cluster", "upgrade"}

	// Subcommands.
	UpgradeCmd.AddCommand(AWSCmd)
	UpgradeCmd.AddCommand(AzureCmd)

	// Flags
	UpgradeCmd.PersistentFlags().
		StringVar(&newKubernetesVersion, constants.FlagNameNewK8sVersion, "", "New Kubernetes version, the cluster will be upgraded to")

	UpgradeCmd.PersistentFlags().
		BoolVar(&skipPRWorkflow, constants.FlagNameSkipPRWorkflow, false,
			"Skip the PR workflow and let KubeAid Bootstrap Script push changes directly to the default branch",
		)
}

func addRequiredFlagsToCommand() {
	if newKubernetesVersion != "" {
		dockerCommand.Cmd = append(dockerCommand.Cmd, fmt.Sprintf("--%s", constants.FlagNameNewK8sVersion))
		dockerCommand.Cmd = append(dockerCommand.Cmd, newKubernetesVersion)
	}
	if skipPRWorkflow {
		dockerCommand.Cmd = append(dockerCommand.Cmd, fmt.Sprintf("--%s", constants.FlagNameSkipPRWorkflow))
	}

	if globals.IsDebugModeEnabled {
		dockerCommand.Cmd = append(dockerCommand.Cmd, "--debug")
	}
}
