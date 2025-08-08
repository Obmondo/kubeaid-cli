package hetznercli

import (
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/docker"
	"github.com/spf13/cobra"
)

var HetznerCmd = &cobra.Command{
	Use: "hetzner",

	Short: "Generate a sample KubeAid Bootstrap Script config file for Hetzner",

	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var dockerCommand docker.CommandOptions

func init() {
	dockerCommand.Cmd = []string{"config", "generate", "hetzner"}

	// Subcommands.
	HetznerCmd.AddCommand(BareMetalCmd)
	HetznerCmd.AddCommand(HCloudCmd)
	HetznerCmd.AddCommand(HybridCmd)
}

func addRequiredFlagsToCommand() {
	if globals.IsDebugModeEnabled {
		dockerCommand.Cmd = append(dockerCommand.Cmd, "--debug")
	}
}
