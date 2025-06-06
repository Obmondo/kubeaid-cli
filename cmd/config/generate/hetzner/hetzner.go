package hetzner

import (
	"github.com/spf13/cobra"
)

var HetznerCmd = &cobra.Command{
	Use: "hetzner",

	Short: "Generate a sample KubeAid Bootstrap Script config file for Hetzner",

	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

func init() {
	// Subcommands.
	HetznerCmd.AddCommand(BareMetalCmd)
	HetznerCmd.AddCommand(HCloudCmd)
	HetznerCmd.AddCommand(HybridCmd)
}
