package create

import (
	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/core"
)

var AzureCmd = &cobra.Command{
	Use: "azure",

	Short: "Create and setup the local K3D management cluster, for deploying an Azure based cluster",

	Run: func(cmd *cobra.Command, args []string) {
		core.CreateDevEnv(cmd.Context(), &core.CreateDevEnvArgs{
			ManagementClusterName:    constants.FlagNameManagementClusterNameDefaultValue,
			SkipMonitoringSetup:      skipMonitoringSetup,
			SkipKubePrometheusBuild:  skipKubePrometheusBuild,
			SkipPRFlow:               skipPRFlow,
			IsPartOfDisasterRecovery: false,
		})
	},
}
