package bootstrap

import (
	"github.com/Obmondo/kubeaid-bootstrap-script/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud/aws"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/core"
	"github.com/spf13/cobra"
)

var AWSCmd = &cobra.Command{
	Use: "aws",
	Run: func(cmd *cobra.Command, args []string) {
		core.BootstrapCluster(cmd.Context(), skipKubeAidConfigSetup, skipClusterctlMove, aws.NewAWSCloudProvider(), false)
	},
}

func init() {
	// Flags.
	config.RegisterAWSCredentialsFlags(AWSCmd)
}
