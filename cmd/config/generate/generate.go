package generate

import (
	"context"
	"log/slog"
	"os"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/spf13/cobra"
)

var GenerateCmd = &cobra.Command{
	Use: "generate",

	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// Verify that file doesn't already exist.
		if _, err := os.Stat(constants.OutputPathGeneratedConfig); err == nil {
			slog.ErrorContext(context.Background(), "Config file already exists", slog.String("path", constants.OutputPathGeneratedConfig))
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
