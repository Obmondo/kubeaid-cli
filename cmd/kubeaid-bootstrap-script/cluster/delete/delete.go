package delete

import (
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/core"
	"github.com/spf13/cobra"
)

var DeleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete a provisioned cluster",
	Run: func(cmd *cobra.Command, args []string) {
		core.DeleteCluster(cmd.Context())
	},
}
