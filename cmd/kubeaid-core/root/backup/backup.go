// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package backup

import (
	"github.com/spf13/cobra"
)

// BackupCmd only groups backup subcommands; unlike the cluster group it has no
// PersistentPreRun, so subcommands run without any parsed cluster config.
var BackupCmd = &cobra.Command{
	Use:   "backup",
	Short: "Inspect backups of a KubeAid managed K8s cluster",
}

func init() {
	BackupCmd.AddCommand(StatusCmd)
}
