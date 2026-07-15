// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package backup

import (
	"github.com/spf13/cobra"
)

var BackupCmd = &cobra.Command{
	Use:   "backup",
	Short: "Inspect backup health for a KubeAid managed cluster",
	Long: `Query the in-cluster backup-exporter and report the health of
PostgreSQL (CNPG logical + WAL) and Velero backups.

These commands are a normal kubectl-style client: they use your current
kube context (standard kubeconfig resolution, honoring $KUBECONFIG and
--context) and are unrelated to cluster bootstrap.`,
}

func init() {
	// Subcommands.
	BackupCmd.AddCommand(StatusCmd)
}
