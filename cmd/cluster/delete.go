package cluster

import (
	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/core"
)

var DeleteCmd = &cobra.Command{
	Use: "delete",

	Short: "Delete a KubeAid managed cluster",

	Run: func(cmd *cobra.Command, args []string) {
		core.DeleteCluster(cmd.Context())
	},
}
