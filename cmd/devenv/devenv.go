package devenv

import (
	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config/parser"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils"
)

var DevenvCmd = &cobra.Command{
	Use: "devenv",

	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// Parse config files.
		parser.ParseConfigFiles(cmd.Context(), globals.ConfigsDirectory)

		// Initialize temp directory.
		utils.InitTempDir(cmd.Context())

		// Ensure required runtime dependencies are installed.
		utils.EnsureRuntimeDependenciesInstalled(cmd.Context())
	},

	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

func init() {
	// Subcommands.
	DevenvCmd.AddCommand(CreateCmd)
}
