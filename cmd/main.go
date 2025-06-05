package main

import (
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-bootstrap-script/cmd/cluster"
	"github.com/Obmondo/kubeaid-bootstrap-script/cmd/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/cmd/devenv"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
)

var RootCmd = &cobra.Command{
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
	RootCmd.AddCommand(config.ConfigCmd)
	RootCmd.AddCommand(devenv.DevenvCmd)
	RootCmd.AddCommand(cluster.ClusterCmd)

	// Flags.

	RootCmd.PersistentFlags().
		BoolVar(&isDebugModeEnabled, constants.FlagNameDebug, false, "Generate debug logs")

	RootCmd.PersistentFlags().
		StringVar(&globals.ConfigsDirectory,
			constants.FlagNameConfigsDirectoy,
			constants.OutputPathGeneratedConfigsDirectory,
			"Path to the directory containing KubeAid Bootstrap Script general and secrets config files",
		)
}

func main() {
	// By default, parent's PersistentPreRun gets overridden by a child's PersistentPreRun.
	// We want to disable this overriding behaviour and chain all the PersistentPreRuns.
	// REFERENCE : https://github.com/spf13/cobra/pull/2044.
	cobra.EnableTraverseRunHooks = true

	err := RootCmd.Execute()
	if err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
}
