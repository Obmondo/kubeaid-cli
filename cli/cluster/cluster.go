package clustercli

import (
	"fmt"

	"github.com/spf13/cobra"

	deletecli "github.com/Obmondo/kubeaid-bootstrap-script/cli/cluster/delete"
	upgradecli "github.com/Obmondo/kubeaid-bootstrap-script/cli/cluster/upgrade"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/docker"
)

var ClusterCmd = &cobra.Command{
	Use: "cluster",
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var dockerCommand docker.CommandOptions

func init() {
	dockerCommand.Cmd = []string{"cluster"}
	// Subcommands.
	ClusterCmd.AddCommand(BootstrapCmd)
	ClusterCmd.AddCommand(TestCmd)
	ClusterCmd.AddCommand(upgradecli.UpgradeCmd)
	ClusterCmd.AddCommand(deletecli.DeleteCmd)
	ClusterCmd.AddCommand(RecoverCmd)

	// Flags.

	ClusterCmd.PersistentFlags().
		BoolVar(&skipPRWorkflow, constants.FlagNameSkipPRWorkflow, false,
			"Skip the PR workflow and let KubeAid Bootstrap Script push changes directly to the default branch",
		)
}

func addRequiredFlagsToCommand() {
	if skipPRWorkflow {
		dockerCommand.Cmd = append(dockerCommand.Cmd, fmt.Sprintf("--%s", constants.FlagNameSkipPRWorkflow))
	}

	// if --configs-directory is passed, we just ensure that the configs' hostPath is mounted in the default configs directory in the image
	if globals.ConfigsDirectory != constants.OutputPathGeneratedConfigsDirectory {
		dockerCommand.HostPath = globals.ConfigsDirectory
		dockerCommand.MountPath = constants.OutputPathGeneratedConfigsDirectory
	}

	if globals.IsDebugModeEnabled {
		dockerCommand.Cmd = append(dockerCommand.Cmd, "--debug")
	}
}
