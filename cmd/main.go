package main

import (
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-bootstrap-script/cmd/cluster"
	"github.com/Obmondo/kubeaid-bootstrap-script/cmd/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/cmd/devenv"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"

	// These packages must be compiled, for the go:linkname directive to work in pkg/config/hack.go.
	_ "github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud/aws"
	_ "github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud/azure"
	_ "github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud/hetzner"
)

var rootCmd = &cobra.Command{
	Use: "kubeaid-bootstrap-script",

	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// Create outputs directory.
		os.MkdirAll(constants.OutputDirectory, os.ModePerm)

		// Initialize logger.
		logger.InitLogger(isDebugModeEnabled)
	},

	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var isDebugModeEnabled bool

func init() {
	// Subcommands.
	rootCmd.AddCommand(config.ConfigCmd)
	rootCmd.AddCommand(devenv.DevenvCmd)
	rootCmd.AddCommand(cluster.ClusterCmd)

	// Flags.
	rootCmd.PersistentFlags().
		BoolVar(&isDebugModeEnabled, constants.FlagNameDebug, false, "Generate debug logs")
}

func main() {
	// By default, parent's PersistentPreRun gets overridden by a child's PersistentPreRun.
	// We want to disable this overriding behaviour and chain all the PersistentPreRuns.
	// REFERENCE : https://github.com/spf13/cobra/pull/2044.
	cobra.EnableTraverseRunHooks = true

	err := rootCmd.Execute()
	if err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
}
