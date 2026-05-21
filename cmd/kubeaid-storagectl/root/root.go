// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package root

import (
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-cli/cmd/kubeaid-storagectl/root/plan"
	"github.com/Obmondo/kubeaid-cli/cmd/kubeaid-storagectl/root/version"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/globals"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/logger"
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
	RootCmd.AddCommand(version.VersionCommand)
	RootCmd.AddCommand(plan.PlanCommand)

	// Flags.

	RootCmd.PersistentFlags().
		BoolVar(&globals.IsDebugModeEnabled, constants.FlagNameDebug, false, "Generate debug logs")
}
