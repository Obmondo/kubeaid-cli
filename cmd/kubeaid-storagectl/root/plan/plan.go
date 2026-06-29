// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package plan

import (
	"context"
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/storageplanner"
	"github.com/Obmondo/kubeaid-cli/pkg/storageplanner/storageplan"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/commandexecutor"
)

var PlanCommand = &cobra.Command{
	Use: "plan",

	// Running `plan` on its own is a read-only dry run: it scans the server's
	// disks and prints the storage allocation it *would* apply, without
	// touching any disk. Use `plan execute` to actually carve the partitions.
	Run: func(cmd *cobra.Command, _ []string) {
		generateAndPrintStoragePlan(cmd.Context(), commandexecutor.NewLocalCommandExecutor(false))
	},
}

var osSize, zfsPoolSize int

// generateAndPrintStoragePlan scans the server's disks (via commandExecutor),
// builds the storage plan and pretty-prints it. It performs NO disk mutation:
// both the dry-run `plan` command and `plan execute` call it to produce and
// show the plan, after which execute additionally applies it. The executor is
// a parameter so callers own its lifecycle and tests can inject a fake.
func generateAndPrintStoragePlan(
	ctx context.Context,
	commandExecutor commandexecutor.CommandExecutor,
) *storageplan.StoragePlan {
	storagePlan, err := storageplanner.GenerateStoragePlan(ctx, "", commandExecutor, osSize, zfsPoolSize)
	assert.AssertErrNil(ctx, err, "Failed generating storage-plan")

	slog.InfoContext(ctx, "Generated storage plan:")
	storagePlan.PrettyPrint()

	return storagePlan
}

func init() {
	// Subcommands.
	PlanCommand.AddCommand(ExecuteCommand)

	// Flags.

	PlanCommand.PersistentFlags().
		IntVar(&osSize, constants.FlagNameOSSize, constants.OSDefaultSize, "OS size (in GB)")

	PlanCommand.PersistentFlags().
		IntVar(&zfsPoolSize, constants.FlagNameZFSPoolSize, constants.ZFSPoolDefaultSize, "ZFS pool size (in GB)")
}
