// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package delete

import (
	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-cli/pkg/core"
)

var MainCmd = &cobra.Command{
	Use: "main",

	Short: "Delete the main KubeAid managed K8s cluster",

	Run: func(cmd *cobra.Command, args []string) {
		core.DeleteCluster(cmd.Context())
	},
}
