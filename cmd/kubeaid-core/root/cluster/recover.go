package cluster

import (
	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/core"
)

var RecoverCmd = &cobra.Command{
	Use: "recover",

	Short: "Recover a KubeAid managed Kubernetes cluster",

	Run: func(cmd *cobra.Command, args []string) {
		core.RecoverCluster(cmd.Context(),
			config.ParsedGeneralConfig.Cluster.Name,
			skipPRWorkflow,
		)
	},
}

var skipPRWorkflow bool

func init() {
	// Flags

	RecoverCmd.PersistentFlags().
		BoolVar(&skipPRWorkflow, constants.FlagNameSkipPRWorkflow, false,
			"Skip the PR workflow and let KubeAid Bootstrap Script push changes directly to the default branch",
		)
}
