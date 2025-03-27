package create

import (
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/spf13/cobra"
)

var CreateCmd = &cobra.Command{
	Use: "create",

	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var (
	managementClusterName string
	skipMonitoringSetup,
	skipKubePrometheusBuild,
	skipPRFlow bool
)

func init() {
	// Subcommands.
	CreateCmd.AddCommand(AWSCmd)
	CreateCmd.AddCommand(AzureCmd)

	// Flags.

	CreateCmd.PersistentFlags().
		StringVar(&managementClusterName, constants.FlagNameManagementClusterName, "test-cluster",
			"Name of the local K3D management cluster",
		)

	CreateCmd.PersistentFlags().
		BoolVar(&skipMonitoringSetup, constants.FlagNameSkipMonitoringSetup, false,
			"Skip KubePrometheus installation",
		)

	CreateCmd.PersistentFlags().
		BoolVar(&skipKubePrometheusBuild, constants.FlagNameSkipKubePrometheusBuild, false,
			"Skip the Kube Prometheus build step while setting up KubeAid Config",
		)

	CreateCmd.PersistentFlags().
		BoolVar(&skipPRFlow, constants.FlagNameSkipPRFlow, false,
			"Skip the PR workflow and let KubeAid Bootstrap Script push changes directly to the default branch",
		)
}
