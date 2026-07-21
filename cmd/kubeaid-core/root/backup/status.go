// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package backup

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/core"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/assert"
)

var StatusCmd = &cobra.Command{
	Use: "status",

	Short: "Show backup health of the cluster (CNPG and Velero), as reported by backup-exporter",

	Args: cobra.NoArgs,

	Run: func(cmd *cobra.Command, args []string) {
		ctx := cmd.Context()

		assert.Assert(ctx,
			outputFormat == "" || outputFormat == "json",
			fmt.Sprintf("invalid --%s value %q: only \"json\" is supported",
				constants.FlagNameOutput, outputFormat),
		)

		core.BackupStatus(ctx, outputFormat)
	},
}

var outputFormat string

func init() {
	StatusCmd.Flags().
		StringVarP(&outputFormat, constants.FlagNameOutput, "o", "",
			`Output format. Only "json" is supported; omit for human-readable output`,
		)
}
