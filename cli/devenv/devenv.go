package devenvcli

import (
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/docker"
	"github.com/spf13/cobra"
)

var DevenvCmd = &cobra.Command{
	Use: "devenv",
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var dockerCommand docker.CommandOptions

func init() {
	// Subcommands.
	dockerCommand.Cmd = []string{"devenv"}

	DevenvCmd.AddCommand(CreateCmd)

	DevenvCmd.PersistentFlags().
		StringVar(&globals.ConfigsDirectory,
			constants.FlagNameConfigsDirectoy,
			constants.OutputPathGeneratedConfigsDirectory,
			"Path to the directory containing KubeAid Bootstrap Script general and secrets config files",
		)
}

func addRequiredFlagsToCommand() {
	// if --configs-directory is passed, we just ensure that the hostPath is mounted in the default configs directory in the image
	if globals.ConfigsDirectory != constants.OutputPathGeneratedConfigsDirectory {
		dockerCommand.HostPath = globals.ConfigsDirectory
		dockerCommand.MountPath = constants.OutputPathGeneratedConfigsDirectory
	}

	if globals.IsDebugModeEnabled {
		dockerCommand.Cmd = append(dockerCommand.Cmd, "--debug")
	}
}
