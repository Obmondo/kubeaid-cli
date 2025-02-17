package cluster

import (
	"github.com/Obmondo/kubeaid-bootstrap-script/cmd/kubeaid-bootstrap-script/cluster/bootstrap"
	delete_ "github.com/Obmondo/kubeaid-bootstrap-script/cmd/kubeaid-bootstrap-script/cluster/delete"
	recover_ "github.com/Obmondo/kubeaid-bootstrap-script/cmd/kubeaid-bootstrap-script/cluster/recover"
	"github.com/Obmondo/kubeaid-bootstrap-script/cmd/kubeaid-bootstrap-script/cluster/upgrade"
	"github.com/Obmondo/kubeaid-bootstrap-script/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/utils"
	"github.com/spf13/cobra"
)

var ClusterCmd = &cobra.Command{
	Use: "cluster",

	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// Initialize config.
		config.InitConfig()

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
	config.RegisterConfigFilePathFlag(ClusterCmd)
}
