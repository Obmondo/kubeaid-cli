package generatecli

import (
	hetznercli "github.com/Obmondo/kubeaid-bootstrap-script/cli/config/generate/hetzner"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/docker"
	"github.com/spf13/cobra"
)

var GenerateCmd = &cobra.Command{
	Use: "generate",
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}
var dockerCommand docker.CommandOptions

func init() {
	dockerCommand.Cmd = []string{"config", "generate"}

	// Subcommands.
	GenerateCmd.AddCommand(AWSCmd)
	GenerateCmd.AddCommand(AzureCmd)
	GenerateCmd.AddCommand(hetznercli.HetznerCmd)
	GenerateCmd.AddCommand(LocalCmd)
	GenerateCmd.AddCommand(BareMetalCmd)
}

func addRequiredFlagsToCommand() {
	if globals.IsDebugModeEnabled {
		dockerCommand.Cmd = append(dockerCommand.Cmd, "--debug")
	}
}
