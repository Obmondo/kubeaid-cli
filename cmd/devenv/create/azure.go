package create

import (
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/core"
	"github.com/spf13/cobra"
)

var AzureCmd = &cobra.Command{
	Use: "azure",

	Short: "Create and setup the local K3D management cluster, for deploying an Azure based cluster",

	Run: func(cmd *cobra.Command, args []string) {
		core.CreateDevEnv(cmd.Context(), constants.FlagNameManagementClusterNameDefaultValue, true, false)
	},
}

func init() {
	// Flags.
	config.RegisterAzureCredentialsFlags(AzureCmd)
}
