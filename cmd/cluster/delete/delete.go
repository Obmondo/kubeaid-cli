package delete

import "github.com/spf13/cobra"

var DeleteCmd = &cobra.Command{
	Use: "delete",

	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

func init() {
	// Subcommands.
	DeleteCmd.AddCommand(MainCmd)
	DeleteCmd.AddCommand(ManagementCmd)
}
