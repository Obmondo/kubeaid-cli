package cluster

import (
	"github.com/Obmondo/kubeaid-bootstrap-script/cmd/cluster/bootstrap"
	delete_ "github.com/Obmondo/kubeaid-bootstrap-script/cmd/cluster/delete"
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
	ClusterCmd.AddCommand(delete_.DeleteCmd)

	// Flags.
	config.RegisterConfigFilePathFlag(ClusterCmd)
}
