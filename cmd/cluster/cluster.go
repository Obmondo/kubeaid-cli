package cluster

import (
	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-bootstrap-script/cmd/cluster/upgrade"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config/parser"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils"
)

var ClusterCmd = &cobra.Command{
	Use: "cluster",

	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// Initialize config.
		parser.ParseConfigFiles(cmd.Context(), config.ConfigsDirectory)

		// Initialize temp directory.
		utils.InitTempDir(cmd.Context())

		// Ensure required runtime dependencies are installed.
		utils.EnsureRuntimeDependenciesInstalled(cmd.Context())
	},

	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

func init() {
	// Subcommands.
	ClusterCmd.AddCommand(BootstrapCmd)
	ClusterCmd.AddCommand(upgrade.UpgradeCmd)
	ClusterCmd.AddCommand(DeleteCmd)
	ClusterCmd.AddCommand(RecoverCmd)

	// Flags.

	config.RegisterConfigsDirectoryFlag(ClusterCmd)

	ClusterCmd.PersistentFlags().
		BoolVar(&skipPRFlow, constants.FlagNameSkipPRFlow, false,
			"Skip the PR workflow and let KubeAid Bootstrap Script push changes directly to the default branch",
		)
}
