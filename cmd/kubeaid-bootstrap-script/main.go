package main

import (
	"log/slog"
	"os"

	"github.com/Obmondo/kubeaid-bootstrap-script/cmd/kubeaid-bootstrap-script/cluster"
	"github.com/Obmondo/kubeaid-bootstrap-script/cmd/kubeaid-bootstrap-script/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/utils/logger"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use: "kubeaid-bootstrap-script",
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

func init() {
	// Subcommands.
	rootCmd.AddCommand(cluster.ClusterCmd)
	rootCmd.AddCommand(config.ConfigCmd)

	// Flags.
	var isDebugModeEnabled bool
	rootCmd.PersistentFlags().
		BoolVar(&isDebugModeEnabled, constants.FlagNameDebug, false, "Run the script in debug mode")

	// Initialization tasks.

	// Initialize logger.
	logger.InitLogger(isDebugModeEnabled)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
}
