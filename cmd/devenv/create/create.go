package create

import (
	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/core"
)

var CreateCmd = &cobra.Command{
	Use: "create",

	Short: "Create and setup the local K3D management cluster, for deploying the KubeAid managed main cluster",

	Run: func(cmd *cobra.Command, args []string) {
		core.CreateDevEnv(cmd.Context(), &core.CreateDevEnvArgs{
			ManagementClusterName:    constants.FlagNameManagementClusterNameDefaultValue,
			SkipMonitoringSetup:      skipMonitoringSetup,
			SkipPRWorkflow:           skipPRWorkflow,
			IsPartOfDisasterRecovery: false,
		})
	},
}

var (
	managementClusterName string
	skipMonitoringSetup,
	skipPRWorkflow bool
)

func init() {
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
		BoolVar(&skipPRWorkflow, constants.FlagNameSkipPRWorkflow, false,
			"Skip the PR workflow and let KubeAid Bootstrap Script push changes directly to the default branch",
		)
}
