package devenv

import (
	"github.com/Obmondo/kubeaid-bootstrap-script/cmd/kubeaid-bootstrap-script/devenv/create"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils"
	"github.com/spf13/cobra"
)

var DevenvCmd = &cobra.Command{
	Use: "devenv",

	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// Initialize config.
		config.InitConfig()

		// Initialize temp directory.
		utils.InitTempDir()
	},

	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

func init() {
	// Subcommands.
	DevenvCmd.AddCommand(create.CreateCmd)

	// Flags.
	config.RegisterConfigFilePathFlag(DevenvCmd)
}
