package recover

import (
	"github.com/spf13/cobra"
)

var RecoverCmd = &cobra.Command{
	Use: "recover",
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

func init() {
	// Subcommands.
	RecoverCmd.AddCommand(AWSCmd)
	RecoverCmd.AddCommand(HetznerCmd)
}
