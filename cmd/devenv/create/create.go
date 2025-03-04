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
	skipKubePrometheusBuild bool
	clusterName             string
)

func init() {
	// Subcommands.
	CreateCmd.AddCommand(AWSCmd)
	CreateCmd.AddCommand(LocalCmd)

	// Flags.
	LocalCmd.PersistentFlags().
		BoolVar(&skipKubePrometheusBuild, constants.FlagNameSkipKubePrometheusBuild, false, "Skip the Kube Prometheus build step while setting up KubeAid Config")

	LocalCmd.PersistentFlags().
		StringVar(&clusterName, "cluster-name", "test-cluster", "Create a local k3d cluster with default argo-cd apps")
}
