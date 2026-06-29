// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package plan

import (
	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-cli/pkg/utils/commandexecutor"
)

var ExecuteCommand = &cobra.Command{
	Use: "execute",

	// execute applies the storage plan: it scans and prints the plan (the same
	// read-only dry run as bare `plan`), then partitions the disks accordingly.
	Run: func(cmd *cobra.Command, _ []string) {
		ctx := cmd.Context()

		commandExecutor := commandexecutor.NewLocalCommandExecutor(false)

		storagePlan := generateAndPrintStoragePlan(ctx, commandExecutor)
		storagePlan.Execute(ctx, commandExecutor)
	},
}
