// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package root

import (
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-bootstrap-script/cmd/kubeaid-storagectl/root/plan"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
)

var RootCmd = &cobra.Command{
	Use: "kubeaid-storagectl",

	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// Create logger.
		logger.CreateLogger(globals.IsDebugModeEnabled, []io.Writer{os.Stdout})
	},

	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

func init() {
	// Subcommands.
	RootCmd.AddCommand(plan.PlanCommand)

	// Flags.

	RootCmd.PersistentFlags().
		BoolVar(&globals.IsDebugModeEnabled, constants.FlagNameDebug, false, "Generate debug logs")
}
