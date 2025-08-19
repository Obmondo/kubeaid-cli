package config

import (
	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-bootstrap-script/cmd/kubeaid-core/root/config/generate"
)

var ConfigCmd = &cobra.Command{
	Use: "config",
}

var ConfigFilesDirectory string

func init() {
	// Subcommands.
	ConfigCmd.AddCommand(generate.GenerateCmd)
}
