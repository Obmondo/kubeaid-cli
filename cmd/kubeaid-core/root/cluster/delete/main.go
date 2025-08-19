package delete

import (
	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/core"
)

var MainCmd = &cobra.Command{
	Use: "main",

	Short: "Delete a KubeAid managed cluster",

	Run: func(cmd *cobra.Command, args []string) {
		core.DeleteCluster(cmd.Context())
	},
}
