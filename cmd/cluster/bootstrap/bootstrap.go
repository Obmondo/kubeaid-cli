package bootstrap

import (
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/spf13/cobra"
)

var BootstrapCmd = &cobra.Command{
	Use: "bootstrap",

	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var (
	managementClusterName string

	skipMonitoringSetup,
	skipKubePrometheusBuild,
	skipPRFlow,
	skipClusterctlMove bool
)

func init() {
	// Subcommands.
	BootstrapCmd.AddCommand(AWSCmd)
	BootstrapCmd.AddCommand(HetznerCmd)
	BootstrapCmd.AddCommand(LocalCmd)

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
		BoolVar(&skipKubePrometheusBuild, constants.FlagNameSkipKubePrometheusBuild, false,
			"Skip the Kube Prometheus build step while setting up KubeAid Config",
		)

	BootstrapCmd.PersistentFlags().
		BoolVar(&skipPRFlow, constants.FlagNameSkipPRFlow, false,
			"Skip the PR workflow and let KubeAid Bootstrap Script push changes directly to the default branch",
		)

	BootstrapCmd.PersistentFlags().
		BoolVar(&skipClusterctlMove, constants.FlagNameSkipClusterctlMove, false,
			"Skip executing the 'clusterctl move' command",
		)
}
