package delete

import (
	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/kubernetes/k3d"
)

var ManagementCmd = &cobra.Command{
	Use: "management",

	Short: "Delete the K3D based local management cluster (used to bootstrap your KubeAid managed main cluster)",

	Run: func(cmd *cobra.Command, args []string) {
		k3d.DeleteK3DCluster(cmd.Context())
	},
}
