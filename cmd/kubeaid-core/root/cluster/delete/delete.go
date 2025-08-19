package delete

import "github.com/spf13/cobra"

var DeleteCmd = &cobra.Command{
	Use: "delete",
}

func init() {
	// Subcommands.
	DeleteCmd.AddCommand(MainCmd)
	DeleteCmd.AddCommand(ManagementCmd)
}
