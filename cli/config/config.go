package configcli

import (
	"github.com/spf13/cobra"

	generatecli "github.com/Obmondo/kubeaid-bootstrap-script/cli/config/generate"
)

var ConfigCmd = &cobra.Command{
	Use: "config",

	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

func init() {
	// Subcommands.
	ConfigCmd.AddCommand(generatecli.GenerateCmd)
}
