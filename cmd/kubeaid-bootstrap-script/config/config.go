package config

import (
	"github.com/Obmondo/kubeaid-bootstrap-script/cmd/kubeaid-bootstrap-script/config/generate"
	"github.com/Obmondo/kubeaid-bootstrap-script/constants"
	"github.com/spf13/cobra"
)

var ConfigCmd = &cobra.Command{
	Use: "config",
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var ConfigFilePath string

func init() {
	// Subcommands.
	ConfigCmd.AddCommand(generate.GenerateCmd)

	// Flags
	ConfigCmd.PersistentFlags().
		StringVar(&ConfigFilePath, constants.FlagNameConfig, constants.OutputPathGeneratedConfig, "Path to the KubeAid Bootstrap Script config file")
}
