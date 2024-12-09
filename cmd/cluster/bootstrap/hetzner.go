package bootstrap

import (
	"github.com/Obmondo/kubeaid-bootstrap-script/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud/hetzner"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/core"
	"github.com/spf13/cobra"
)

var HetznerCmd = &cobra.Command{
	Use: "hetzner",
	Run: func(cmd *cobra.Command, args []string) {
		core.BootstrapCluster(cmd.Context(), skipKubeAidConfigSetup, skipClusterctlMove, hetzner.NewHetznerCloudProvider(), false)
	},
}

func init() {
	// Flags.
	config.RegisterHetznerCredentialsFlags(HetznerCmd)
}
