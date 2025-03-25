package recover

import (
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/spf13/cobra"
)

var RecoverCmd = &cobra.Command{
	Use: "recover",
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var skipPRFlow bool

func init() {
	// Subcommands.
	RecoverCmd.AddCommand(AWSCmd)
	RecoverCmd.AddCommand(HetznerCmd)

	// Flags

	RecoverCmd.PersistentFlags().
		BoolVar(&skipPRFlow, constants.FlagNameSkipPRFlow, false,
			"Skip the PR workflow and let KubeAid Bootstrap Script push changes directly to the default branch",
		)
}
