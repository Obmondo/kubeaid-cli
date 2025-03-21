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
	managementClusterName   string
	skipKubePrometheusBuild bool
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
		BoolVar(&skipKubePrometheusBuild, constants.FlagNameSkipKubePrometheusBuild, false,
			"Skip the Kube Prometheus build step while setting up KubeAid Config",
		)
}
