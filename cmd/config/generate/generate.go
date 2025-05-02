package generate

import (
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
)

var GenerateCmd = &cobra.Command{
	Use: "generate",

	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// Verify that config files directory doesn't already exist.
		if _, err := os.Stat(constants.OutputPathGeneratedConfigsDirectory); err == nil {
			slog.ErrorContext(
				cmd.Context(),
				"Config files directory already exists",
				slog.String("path", constants.OutputPathGeneratedConfigsDirectory),
			)
			os.Exit(1)
		}
	},

	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

func init() {
	// Subcommands.
	GenerateCmd.AddCommand(AWSCmd)
	GenerateCmd.AddCommand(HetznerCmd)
	GenerateCmd.AddCommand(LocalCmd)
}
