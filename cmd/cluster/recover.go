package cluster

import (
	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/core"
)

var RecoverCmd = &cobra.Command{
	Use: "recover",

	Short: "Recover a KubeAid managed Kubernetes cluster",

	Run: func(cmd *cobra.Command, args []string) {
		core.RecoverCluster(
			cmd.Context(),
			constants.FlagNameManagementClusterNameDefaultValue,
			skipPRFlow,
		)
	},
}

var skipPRFlow bool

func init() {
	// Flags

	RecoverCmd.PersistentFlags().
		BoolVar(&skipPRFlow, constants.FlagNameSkipPRFlow, false,
			"Skip the PR workflow and let KubeAid Bootstrap Script push changes directly to the default branch",
		)
}
