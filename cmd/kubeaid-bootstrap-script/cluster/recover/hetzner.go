package recover

import (
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/core"
	"github.com/spf13/cobra"
)

var HetznerCmd = &cobra.Command{
	Use: "hetzner",
	Run: func(cmd *cobra.Command, args []string) {
		core.RecoverCluster(cmd.Context())
	},
}

func init() {
	// Flags.
	config.RegisterHetznerCredentialsFlags(HetznerCmd)
}
