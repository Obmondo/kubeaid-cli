package config

import (
	"github.com/Obmondo/kubeaid-bootstrap-script/cmd/config/generate"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/spf13/cobra"
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

	// Flags
	ConfigCmd.PersistentFlags().
		StringVar(
			&ConfigFilesDirectory,
			constants.FlagNameConfigsDirectoy,
			constants.OutputPathGeneratedConfigsDirectory,
			"Path to the directory containing KubeAid Bootstrap Script general and secrets config files",
		)
}
