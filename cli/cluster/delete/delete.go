package deletecli

import (
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/docker"
	"github.com/spf13/cobra"
)

var DeleteCmd = &cobra.Command{
	Use: "delete",

	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var dockerCommand docker.CommandOptions

func init() {
	dockerCommand.Cmd = []string{"cluster", "delete"}

	// Subcommands.
	DeleteCmd.AddCommand(MainCmd)
	DeleteCmd.AddCommand(ManagementCmd)
}

func addRequiredFlagsToCommand() {
	if globals.IsDebugModeEnabled {
		dockerCommand.Cmd = append(dockerCommand.Cmd, "--debug")
	}

	// if --configs-directory is passed, we just ensure that the hostPath is mounted in the default configs directory in the image
	if globals.ConfigsDirectory != constants.OutputPathGeneratedConfigsDirectory {
		dockerCommand.HostPath = globals.ConfigsDirectory
		dockerCommand.MountPath = constants.OutputPathGeneratedConfigsDirectory
	}
}
