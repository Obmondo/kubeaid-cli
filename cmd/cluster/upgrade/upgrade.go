package upgrade

import (
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/spf13/cobra"
)

var UpgradeCmd = &cobra.Command{
	Use: "upgrade",

	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var kubernetesVersion string

func init() {
	// Subcommands.
	UpgradeCmd.AddCommand(AWSCmd)

	// Flags.

	UpgradeCmd.PersistentFlags().
		StringVar(&kubernetesVersion, constants.FlagNameK8sVersion, "", "New Kubernetes version the cluster will be upgraded to")
}
