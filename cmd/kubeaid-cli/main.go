// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package main

import (
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	kubeaidCoreRoot "github.com/Obmondo/kubeaid-cli/cmd/kubeaid-core/root"
	"github.com/Obmondo/kubeaid-cli/cmd/kubeaid-cli/login"
	_ "github.com/Obmondo/kubeaid-cli/internal/termsetup"
)

// buildRootCmd assembles the kubeaid-cli command tree: the shared
// kubeaid-core root plus the kubeaid-cli-only subcommands. Kept separate
// from main() so a test can assert the tree is wired correctly (see
// main_test.go) — the login subcommand was silently dropped once already
// in the single-binary refactor (af26b61), and the regression test
// guards against that recurring.
func buildRootCmd() *cobra.Command {
	rootCmd := kubeaidCoreRoot.RootCmd
	rootCmd.Use = "kubeaid-cli"

	// login runs entirely locally (no Docker, no general.yaml parsing) —
	// it reads the klist config repo and writes a kubeconfig. It lives
	// under cmd/kubeaid-cli/ rather than kubeaid-core/root/ because it's
	// a kubeaid-cli-only entrypoint; register it here.
	rootCmd.AddCommand(login.LoginCmd)

	return rootCmd
}

func main() {
	//nolint:reassign
	// By default, parent's PersistentPreRun gets overridden by a child's PersistentPreRun.
	// We want to disable this overriding behaviour and chain all the PersistentPreRuns.
	// REFERENCE : https://github.com/spf13/cobra/pull/2044.
	cobra.EnableTraverseRunHooks = true

	err := buildRootCmd().Execute()
	if err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
}
