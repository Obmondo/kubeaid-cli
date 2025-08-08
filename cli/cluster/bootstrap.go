package clustercli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/docker"
)

var BootstrapCmd = &cobra.Command{
	Use: "bootstrap",

	Short: "Bootstrap a KubeAid managed cluster",

	Run: func(cmd *cobra.Command, args []string) {
		dockerCommand.Cmd = append(dockerCommand.Cmd, "bootstrap")

		addRequiredFlagsToCommand()

		if managementClusterName != constants.FlagNameManagementClusterNameDefaultValue {
			dockerCommand.Cmd = append(dockerCommand.Cmd, fmt.Sprintf("--%s", constants.FlagNameManagementClusterName))
			dockerCommand.Cmd = append(dockerCommand.Cmd, managementClusterName)
		}

		if skipMonitoringSetup {
			dockerCommand.Cmd = append(dockerCommand.Cmd, fmt.Sprintf("--%s", constants.FlagNameSkipMonitoringSetup))
		}

		if skipClusterctlMove {
			dockerCommand.Cmd = append(dockerCommand.Cmd, fmt.Sprintf("--%s", constants.FlagNameSkipClusterctlMove))
		}

		docker.ExecuteDockerCommand(dockerCommand)
	},
}

var (
	managementClusterName string

	skipMonitoringSetup,
	skipKubePrometheusBuild,
	skipClusterctlMove bool
)

func init() {
	// Flags.
	BootstrapCmd.PersistentFlags().
		StringVar(&managementClusterName, constants.FlagNameManagementClusterName, constants.FlagNameManagementClusterNameDefaultValue,
			"Name of the local K3D management cluster",
		)

	BootstrapCmd.PersistentFlags().
		BoolVar(&skipMonitoringSetup, constants.FlagNameSkipMonitoringSetup, false,
			"Skip KubePrometheus installation",
		)

	BootstrapCmd.PersistentFlags().
		BoolVar(&skipClusterctlMove, constants.FlagNameSkipClusterctlMove, false,
			"Skip executing the 'clusterctl move' command",
		)
}
