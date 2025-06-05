package cluster

import (
	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/core"
)

var BootstrapCmd = &cobra.Command{
	Use: "bootstrap",

	Short: "Bootstrap a Kubernetes cluster and setup KubeAid",

	Run: func(cmd *cobra.Command, args []string) {
		core.BootstrapCluster(cmd.Context(), core.BootstrapClusterArgs{
			CreateDevEnvArgs: &core.CreateDevEnvArgs{
				ManagementClusterName:    managementClusterName,
				SkipMonitoringSetup:      skipMonitoringSetup,
				SkipPRWorkflow:           skipPRWorkflow,
				IsPartOfDisasterRecovery: false,
			},
			SkipClusterctlMove: skipClusterctlMove,
		})
	},
}

var (
	managementClusterName string

	skipMonitoringSetup,
	skipKubePrometheusBuild,
	skipClusterctlMove bool
)

func init() {
	// Flags.

	BootstrapCmd.PersistentFlags().
		StringVar(&managementClusterName, constants.FlagNameManagementClusterName, constants.FlagNameManagementClusterNameDefaultValue,
			"Name of the local K3D management cluster",
		)

	BootstrapCmd.PersistentFlags().
		BoolVar(&skipMonitoringSetup, constants.FlagNameSkipMonitoringSetup, false,
			"Skip KubePrometheus installation",
		)

	BootstrapCmd.PersistentFlags().
		BoolVar(&skipClusterctlMove, constants.FlagNameSkipClusterctlMove, false,
			"Skip executing the 'clusterctl move' command",
		)
}
