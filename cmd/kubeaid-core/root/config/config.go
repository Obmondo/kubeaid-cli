// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package config

import (
	"github.com/spf13/cobra"
)

var ConfigCmd = &cobra.Command{
	Use: "config",
}

var ConfigFilesDirectory string

func init() {
	// Subcommands.
	ConfigCmd.AddCommand(GenerateCmd)
}
