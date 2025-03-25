package bootstrap

import (
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/core"
	"github.com/spf13/cobra"
)

var HetznerCmd = &cobra.Command{
	Use: "hetzner",

	Short: "Bootstrap a Hetzner based cluster",

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

func init() {
	// Flags.
	config.RegisterHetznerCredentialsFlags(HetznerCmd)
}
