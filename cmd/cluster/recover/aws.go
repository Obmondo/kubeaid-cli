package recover

import (
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/core"
	"github.com/spf13/cobra"
)

var AWSCmd = &cobra.Command{
	Use: "aws",
	Run: func(cmd *cobra.Command, args []string) {
		core.RecoverCluster(cmd.Context(), constants.FlagNameManagementClusterNameDefaultValue, skipPRFlow)
	},
}

func init() {
	// Flags.
	config.RegisterAWSCredentialsFlags(AWSCmd)
}
