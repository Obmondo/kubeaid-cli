// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package cluster

import (
	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/core"
)

var TestCmd = &cobra.Command{
	Use: "test",

	Short: "Test whether KubeAid Bootstrap Script properly bootstrapped your cluster or not",

	Run: func(cmd *cobra.Command, args []string) {
		core.TestCluster(cmd.Context())
	},
}
