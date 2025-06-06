package upgrade

import (
	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
)

var UpgradeCmd = &cobra.Command{
	Use: "upgrade",

	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var (
	kubernetesVersion string
	skipPRWorkflow    bool
)

func init() {
	// Subcommands.
	UpgradeCmd.AddCommand(AWSCmd)

	// Flags.

	UpgradeCmd.PersistentFlags().
		StringVar(&kubernetesVersion, constants.FlagNameK8sVersion, "", "New Kubernetes version the cluster will be upgraded to")

	UpgradeCmd.PersistentFlags().
		BoolVar(&skipPRWorkflow, constants.FlagNameSkipPRWorkflow, false,
			"Skip the PR workflow and let KubeAid Bootstrap Script push changes directly to the default branch",
		)
}
