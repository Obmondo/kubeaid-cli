package cluster

import (
	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-bootstrap-script/cmd/cluster/delete"
	"github.com/Obmondo/kubeaid-bootstrap-script/cmd/cluster/upgrade"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config/parser"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils"
)

var ClusterCmd = &cobra.Command{
	Use: "cluster",

	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// Parse config files.
		parser.ParseConfigFiles(cmd.Context(), globals.ConfigsDirectory)

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
	ClusterCmd.AddCommand(TestCmd)
	ClusterCmd.AddCommand(upgrade.UpgradeCmd)
	ClusterCmd.AddCommand(delete.DeleteCmd)
	ClusterCmd.AddCommand(RecoverCmd)

	// Flags.

	ClusterCmd.PersistentFlags().
		BoolVar(&skipPRWorkflow, constants.FlagNameSkipPRWorkflow, false,
			"Skip the PR workflow and let KubeAid Bootstrap Script push changes directly to the default branch",
		)
}
