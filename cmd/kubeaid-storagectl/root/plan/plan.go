// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package plan

import (
	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
)

var PlanCommand = &cobra.Command{
	Use: "plan",

	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var osSize, zfsPoolSize int

func init() {
	// Subcommands.
	PlanCommand.AddCommand(ExecuteCommand)

	// Flags.

	PlanCommand.PersistentFlags().
		IntVar(&osSize, constants.FlagNameOSSize, constants.OSDefaultSize, "OS size")

	PlanCommand.PersistentFlags().
		IntVar(&zfsPoolSize, constants.FlagNameZFSPoolSize, constants.ZFSPoolDefaultSize, "ZFS pool size")
}
