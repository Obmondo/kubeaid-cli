package root

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-bootstrap-script/cmd/kubeaid-core/root/cluster"
	"github.com/Obmondo/kubeaid-bootstrap-script/cmd/kubeaid-core/root/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/cmd/kubeaid-core/root/devenv"
	"github.com/Obmondo/kubeaid-bootstrap-script/cmd/kubeaid-core/root/version"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
)

var RootCmd = &cobra.Command{
	Use: "kubeaid-core",

	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// nolint: revive
		// Create outputs directory.
		os.MkdirAll(constants.OutputsDirectory, os.ModePerm)

		// Initialize logger.
		logger.InitLogger(globals.IsDebugModeEnabled)
	},

	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

func init() {
	// Subcommands.
	RootCmd.AddCommand(config.ConfigCmd)
	RootCmd.AddCommand(devenv.DevenvCmd)
	RootCmd.AddCommand(cluster.ClusterCmd)
	RootCmd.AddCommand(version.VersionCommand)

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
