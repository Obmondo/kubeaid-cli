package create

import (
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/core"
	"log/slog"

	"github.com/spf13/cobra"
)

var LocalCmd = &cobra.Command{
	Use: "local",

	Short: "Create and setup the local K3D cluster, with boostrap argocd apps",

	Run: func(cmd *cobra.Command, args []string) {
		core.CreateDevEnv(cmd.Context(), clusterName, true, false)
		slog.Info("Local cluster bootstrapping finished 🎊")
	},
}
