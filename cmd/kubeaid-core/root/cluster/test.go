// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package cluster

import (
	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-cli/pkg/core"
)

var TestCmd = &cobra.Command{
	Use: "test",

	Short: "Test a KubeAid managed K8s cluster (verify it was bootstrapped properly)",

	Run: func(cmd *cobra.Command, args []string) {
		core.TestCluster(cmd.Context())
	},
}
