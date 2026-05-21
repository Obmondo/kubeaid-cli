// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package main

import (
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	kubeaidCoreRoot "github.com/Obmondo/kubeaid-cli/cmd/kubeaid-core/root"
	_ "github.com/Obmondo/kubeaid-cli/internal/termsetup"
)

func main() {
	//nolint:reassign
	// By default, parent's PersistentPreRun gets overridden by a child's PersistentPreRun.
	// We want to disable this overriding behaviour and chain all the PersistentPreRuns.
	// REFERENCE : https://github.com/spf13/cobra/pull/2044.
	cobra.EnableTraverseRunHooks = true

	rootCmd := kubeaidCoreRoot.RootCmd
	rootCmd.Use = "kubeaid-cli"

	err := rootCmd.Execute()
	if err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
}
