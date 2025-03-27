package bootstrap

import (
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/core"

	"github.com/spf13/cobra"
)

var LocalCmd = &cobra.Command{
	Use: "local",

	Short: "Bootstrap a local K3D cluster (for internal testing purposes)",

	Run: func(cmd *cobra.Command, args []string) {
		core.BootstrapCluster(cmd.Context(), core.BootstrapClusterArgs{
			CreateDevEnvArgs: &core.CreateDevEnvArgs{
				ManagementClusterName:    managementClusterName,
				SkipMonitoringSetup:      skipMonitoringSetup,
				SkipKubePrometheusBuild:  skipKubePrometheusBuild,
				SkipPRFlow:               skipPRFlow,
				IsPartOfDisasterRecovery: false,
			},
			SkipClusterctlMove: skipClusterctlMove,
		})
	},
}
