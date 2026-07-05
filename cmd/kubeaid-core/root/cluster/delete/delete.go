// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package delete

import "github.com/spf13/cobra"

var DeleteCmd = &cobra.Command{
	Use: "delete",

	Short: "Delete a KubeAid managed K8s cluster (main, or the local K3D management cluster)",
}

func init() {
	// Subcommands.
	DeleteCmd.AddCommand(MainCmd)
	DeleteCmd.AddCommand(ManagementCmd)
}
