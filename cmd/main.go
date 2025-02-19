package main

import (
	"log/slog"
	"os"

	"github.com/Obmondo/kubeaid-bootstrap-script/cmd/cluster"
	"github.com/Obmondo/kubeaid-bootstrap-script/cmd/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/cmd/devenv"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
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
	rootCmd.AddCommand(config.ConfigCmd)
	rootCmd.AddCommand(devenv.DevenvCmd)
	rootCmd.AddCommand(cluster.ClusterCmd)

	// Flags.
	var isDebugModeEnabled bool
	rootCmd.PersistentFlags().
		BoolVar(&isDebugModeEnabled, constants.FlagNameDebug, false, "Generate debug logs")

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
