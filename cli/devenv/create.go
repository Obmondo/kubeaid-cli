package devenvcli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/docker"
)

var CreateCmd = &cobra.Command{
	Use: "create",

	Short: "Create and setup the local K3D management cluster, for deploying the KubeAid managed main cluster",

	Run: func(cmd *cobra.Command, args []string) {
		dockerCommand.Cmd = append(dockerCommand.Cmd, "create")

		addRequiredFlagsToCommand()

		if managementClusterName != constants.FlagNameManagementClusterName {
			dockerCommand.Cmd = append(dockerCommand.Cmd, fmt.Sprintf("--%s", constants.FlagNameManagementClusterName))
			dockerCommand.Cmd = append(dockerCommand.Cmd, managementClusterName)
		}

		if skipMonitoringSetup {
			dockerCommand.Cmd = append(dockerCommand.Cmd, fmt.Sprintf("--%s", constants.FlagNameSkipMonitoringSetup))
		}

		if skipPRWorkflow {
			dockerCommand.Cmd = append(dockerCommand.Cmd, fmt.Sprintf("--%s", constants.FlagNameSkipPRWorkflow))
		}

		docker.ExecuteDockerCommand(dockerCommand)
	},
}

var (
	managementClusterName string
	skipMonitoringSetup,
	skipPRWorkflow bool
)

func init() {
	// Flags.
	CreateCmd.Flags().
		StringVar(&managementClusterName, constants.FlagNameManagementClusterName, constants.FlagNameManagementClusterNameDefaultValue,
			"Name of the local K3D management cluster",
		)

	CreateCmd.Flags().
		BoolVar(&skipMonitoringSetup, constants.FlagNameSkipMonitoringSetup, false,
			"Skip KubePrometheus installation",
		)

	CreateCmd.Flags().
		BoolVar(&skipPRWorkflow, constants.FlagNameSkipPRWorkflow, false,
			"Skip the PR workflow and let KubeAid Bootstrap Script push changes directly to the default branch",
		)
}
