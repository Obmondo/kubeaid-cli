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
		// Create logger. CreateLogger expects {fileWriter, stdoutWriter}
		// and indexes writers[1] — passing only os.Stdout panicked with
		// `index out of range [1] with length 1` on every node bootstrap
		// (cloud-init runcmd invocation), turning the storage-plan step
		// into a hard failure even on otherwise-healthy nodes. Storagectl
		// runs inside cloud-init where stdout already lands in
		// /var/log/cloud-init-output.log, so we want NO separate log file
		// — discard the file writer and route the stdout writer through.
		// Same convention as the generator tool (tools/generators/cmd/main.go).
		logger.CreateLogger(globals.IsDebugModeEnabled, []io.Writer{io.Discard, os.Stdout})
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
