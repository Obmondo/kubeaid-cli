package bootstrap

import (
	"github.com/Obmondo/kubeaid-bootstrap-script/constants"
	"github.com/spf13/cobra"
)

var BootstrapCmd = &cobra.Command{
	Use: "bootstrap",
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var (
	skipKubeAidConfigSetup,
	skipClusterctlMove bool
)

func init() {
	// Subcommands.
	BootstrapCmd.AddCommand(AWSCmd)
	BootstrapCmd.AddCommand(HetznerCmd)

	// Flags.

	BootstrapCmd.PersistentFlags().
		BoolVar(&skipKubeAidConfigSetup, constants.FlagNameSkipKubeAidConfigSetup, false, "Skip the initial KubeAid config repo setup step")

	BootstrapCmd.PersistentFlags().
		BoolVar(&skipClusterctlMove, constants.FlagNameSkipClusterctlMove, false, "Skip executing the 'clusterctl move' command")
}
