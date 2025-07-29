package delete

import (
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/kubernetes/k3d"
	"github.com/spf13/cobra"
)

var ManagementCmd = &cobra.Command{
	Use: "management",

	Short: "Delete the K3D based local management cluster (used to bootstrap your KubeAid managed main cluster)",

	Run: func(cmd *cobra.Command, args []string) {
		k3d.DeleteK3DCluster(cmd.Context())
	},
}
