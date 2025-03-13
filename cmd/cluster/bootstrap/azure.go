package bootstrap

import (
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/core"
	"github.com/spf13/cobra"
)

var AzureCmd = &cobra.Command{
	Use: "azure",

	Short: "Bootstrap an Azure based cluster",

	Run: func(cmd *cobra.Command, args []string) {
		core.BootstrapCluster(cmd.Context(),
			constants.FlagNameManagementClusterNameDefaultValue,
			skipKubePrometheusBuild,
			skipClusterctlMove,
			false,
		)
	},
}

func init() {
	// Flags.
	config.RegisterAzureCredentialsFlags(AzureCmd)
}
