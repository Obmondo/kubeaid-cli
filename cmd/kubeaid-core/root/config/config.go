// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package config

import (
	"github.com/spf13/cobra"
)

var ConfigCmd = &cobra.Command{
	Use:   "config",
	Short: "Generate and manage required configuration files (i.e. the general.yaml and secrets.yaml files)",
}

var ConfigFilesDirectory string

func init() {
	// Subcommands.
	ConfigCmd.AddCommand(GenerateCmd)
}
