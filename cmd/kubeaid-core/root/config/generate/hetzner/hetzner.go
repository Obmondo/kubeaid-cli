// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package hetzner

import (
	"github.com/spf13/cobra"
)

var HetznerCmd = &cobra.Command{
	Use: "hetzner",

	Short: "Generate a sample KubeAid Bootstrap Script config file for Hetzner",
}

func init() {
	// Subcommands.
	HetznerCmd.AddCommand(BareMetalCmd)
	HetznerCmd.AddCommand(HCloudCmd)
	HetznerCmd.AddCommand(HybridCmd)
}
