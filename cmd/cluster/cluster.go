package cluster

import (
	"github.com/Obmondo/kubeaid-bootstrap-script/cmd/cluster/bootstrap"
	delete_ "github.com/Obmondo/kubeaid-bootstrap-script/cmd/cluster/delete"
	recover_ "github.com/Obmondo/kubeaid-bootstrap-script/cmd/cluster/recover"
	"github.com/Obmondo/kubeaid-bootstrap-script/cmd/cluster/upgrade"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils"
	"github.com/spf13/cobra"
)

var ClusterCmd = &cobra.Command{
	Use: "cluster",

	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// Initialize config.
		config.ParseConfigFiles(cmd.Context(), config.ConfigsDirectory)

		// Initialize temp directory.
		utils.InitTempDir()
	},

	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

func init() {
	// Subcommands.
	ClusterCmd.AddCommand(bootstrap.BootstrapCmd)
	ClusterCmd.AddCommand(upgrade.UpgradeCmd)
	ClusterCmd.AddCommand(delete_.DeleteCmd)
	ClusterCmd.AddCommand(recover_.RecoverCmd)

	// Flags.
	config.RegisterConfigsDirectoryFlag(ClusterCmd)
}
