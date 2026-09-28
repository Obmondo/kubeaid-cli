// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package siem

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/siemctl"
)

var backupFlags struct {
	name           string
	veleroSchedule string
	skipVelero     bool
	wait           bool
	timeout        time.Duration
	namespaces     []string
	confirm        bool
}

// BackupCmd is `siem backup`.
var BackupCmd = &cobra.Command{
	Use:   "backup",
	Short: "Back up the security operations stack now",
	Long: `Creates one backup set (label kubesoc.io/backup-set=<name>) of:

  - a Velero Backup of security-operations and every wazuh-<code> namespace,
    from the template of the Velero Schedule that covers security-operations
    (or --velero-schedule), else a plain Backup with a 30 day TTL
  - a CloudNativePG Backup per PostgreSQL cluster with backups configured
  - a MariaDB Backup per MariaDB, copying the storage of its scheduled Backup
  - one run of every CronJob labelled ` + siemctl.SnapshotCronJobLabel + ` (OpenSearch snapshots)

--dry-run lists the objects without creating them. --wait waits for the
Velero Backup to finish.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		return runBackup(cmd, tenantCodes(cfg), backupFlags.name)
	},
}

// runBackup plans and (unless --dry-run) creates a backup set.
func runBackup(cmd *cobra.Command, tenants []string, name string) error {
	ctx := cmd.Context()
	c, err := newClients(ctx)
	if err != nil {
		return err
	}
	plan, err := siemctl.PlanBackup(ctx, siemctl.BackupOptions{
		Dynamic:        c.dynamic,
		Tenants:        tenants,
		Name:           name,
		VeleroSchedule: backupFlags.veleroSchedule,
		SkipVelero:     backupFlags.skipVelero,
	})
	if err != nil {
		return err
	}
	plan.Print(cmd.OutOrStdout())
	if flagDryRun {
		return nil
	}
	if len(plan.Objects) == 0 {
		return errors.New("nothing to back up")
	}
	if err := plan.Execute(ctx, c.dynamic); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Created backup set %s.\n", plan.Name)
	if backupFlags.wait && !backupFlags.skipVelero {
		if err := waitForVeleroBackup(ctx, c, plan.Name, backupFlags.timeout); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Velero Backup %s completed.\n", plan.Name)
	}
	return nil
}

// waitForVeleroBackup polls the Backup's status.phase until Completed.
func waitForVeleroBackup(ctx context.Context, c *clients, name string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		b, err := c.dynamic.Resource(siemctl.VeleroBackupGVR).Namespace(constants.NamespaceVelero).
			Get(ctx, name, metaV1.GetOptions{})
		if err == nil {
			phase, _, _ := unstructured.NestedString(b.Object, "status", "phase")
			switch phase {
			case "Completed":
				return nil
			case "Failed", "PartiallyFailed", "FailedValidation":
				return fmt.Errorf("velero Backup %s: %s", name, phase)
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for velero Backup %s: %w", name, ctx.Err())
		case <-time.After(10 * time.Second):
		}
	}
}

// RestoreCmd is `siem restore`.
var RestoreCmd = &cobra.Command{
	Use:   "restore <backup-set>",
	Short: "Restore a backup set (needs --confirm)",
	Long: `Restores a backup set made by 'siem backup':

  - a Velero Restore of the set's Velero Backup (--namespace limits it; objects
    that still exist are left alone: existingResourcePolicy none)
  - a MariaDB Restore per MariaDB Backup of the set

PostgreSQL (CloudNativePG) restores into a new cluster only, and OpenSearch
snapshots through the indexer's _snapshot API: the command prints those steps.
--dry-run lists the objects without creating them.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if !backupFlags.confirm && !flagDryRun {
			return errors.New("a restore overwrites data: pass --confirm (or --dry-run to see the plan)")
		}
		ctx := cmd.Context()
		c, err := newClients(ctx)
		if err != nil {
			return err
		}
		plan, err := siemctl.PlanRestore(ctx, siemctl.RestoreOptions{
			Dynamic:    c.dynamic,
			Backup:     args[0],
			Namespaces: backupFlags.namespaces,
		})
		if err != nil {
			return err
		}
		plan.Print(cmd.OutOrStdout())
		if flagDryRun {
			return nil
		}
		if err := confirm(cmd, fmt.Sprintf("Create the %d restore objects above?", len(plan.Objects))); err != nil {
			return err
		}
		return plan.Execute(ctx, c.dynamic)
	},
}

func init() {
	SiemCmd.AddCommand(BackupCmd, RestoreCmd)

	addClusterDirFlags(BackupCmd)
	addDryRunFlag(BackupCmd, "List the backup objects and create nothing")
	f := BackupCmd.Flags()
	f.StringVar(&backupFlags.name, "name", "", "Backup set name. Default: kubesoc-<UTC time>")
	f.StringVar(&backupFlags.veleroSchedule, "velero-schedule", "", "Velero Schedule whose template the Backup copies")
	f.BoolVar(&backupFlags.skipVelero, "skip-velero", false, "Leave out the Velero Backup")
	f.BoolVar(&backupFlags.wait, "wait", false, "Wait for the Velero Backup to complete")
	f.DurationVar(&backupFlags.timeout, "timeout", 2*time.Hour, "How long --wait waits")

	addDryRunFlag(RestoreCmd, "List the restore objects and create nothing")
	addYesFlag(RestoreCmd, "restoring")
	f = RestoreCmd.Flags()
	f.BoolVar(&backupFlags.confirm, "confirm", false, "Required: confirm the restore")
	f.StringSliceVar(&backupFlags.namespaces, "namespace", nil, "Restore only these namespaces (repeatable)")
}
