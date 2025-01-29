package create

import (
	"github.com/spf13/cobra"
)

var CreateCmd = &cobra.Command{
	Use: "create",
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

func init() {
	// Subcommands.
	CreateCmd.AddCommand(AWSCmd)
}
