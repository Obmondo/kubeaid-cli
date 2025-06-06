package config

import (
	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-bootstrap-script/cmd/config/generate"
)

var ConfigCmd = &cobra.Command{
	Use: "config",

	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var ConfigFilesDirectory string

func init() {
	// Subcommands.
	ConfigCmd.AddCommand(generate.GenerateCmd)
}
