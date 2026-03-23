// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package plan

import (
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/storageplanner"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/commandexecutor"
)

var ExecuteCommand = &cobra.Command{
	Use: "execute",

	Run: func(cmd *cobra.Command, args []string) {
		ctx := cmd.Context()

		commandExecutor := commandexecutor.NewLocalCommandExecutor(false)

		storagePlan, err := storageplanner.GenerateStoragePlan(ctx, "", commandExecutor, osSize, zfsPoolSize)
		assert.AssertErrNil(ctx, err, "Failed generating storage-plan")

		slog.InfoContext(ctx, "Generated storage-plan : ")
		storagePlan.PrettyPrint()

		storagePlan.Execute(ctx, commandExecutor)
	},
}
