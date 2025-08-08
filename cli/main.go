package main

import (
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	clustercli "github.com/Obmondo/kubeaid-bootstrap-script/cli/cluster"
	configcli "github.com/Obmondo/kubeaid-bootstrap-script/cli/config"
	devenvcli "github.com/Obmondo/kubeaid-bootstrap-script/cli/devenv"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
)

var RootCmd = &cobra.Command{
	Use: "kubeaid-cli",
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

func init() {
	// Subcommands.
	RootCmd.AddCommand(configcli.ConfigCmd)
	RootCmd.AddCommand(devenvcli.DevenvCmd)
	RootCmd.AddCommand(clustercli.ClusterCmd)

	// Flags.
	RootCmd.PersistentFlags().
		BoolVar(&globals.IsDebugModeEnabled, constants.FlagNameDebug, false, "Generate debug logs")

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
	// cobra.EnableTraverseRunHooks = true

	err := RootCmd.Execute()
	if err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
}
