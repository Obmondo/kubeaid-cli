package bootstrap

import (
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/core"

	"github.com/spf13/cobra"
)

var LocalCmd = &cobra.Command{
	Use: "local",

	Short: "Bootstrap a local K3D cluster (for internal testing purposes)",

	Run: func(cmd *cobra.Command, args []string) {
		core.BootstrapCluster(cmd.Context(),
			constants.FlagNameManagementClusterNameDefaultValue,
			skipKubePrometheusBuild,
			skipClusterctlMove,
			false,
		)
	},
}
