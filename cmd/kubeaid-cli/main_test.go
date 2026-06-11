// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package main

import "testing"

// TestBuildRootCmdRegistersLogin guards against the login subcommand
// being silently dropped from the kubeaid-cli binary. It was removed
// once already as collateral in the single-binary refactor (af26b61),
// leaving the login package in the tree but unreachable from the CLI.
func TestBuildRootCmdRegistersLogin(t *testing.T) {
	t.Parallel()

	rootCmd := buildRootCmd()

	for _, subCmd := range rootCmd.Commands() {
		if subCmd.Name() == "login" {
			return
		}
	}

	t.Fatal("login subcommand is not registered on the kubeaid-cli root command")
}
