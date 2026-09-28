// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package siem

import (
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/siemctl"
)

var upgradeFlags struct {
	chartRevision string
	reconcilerTag string
	skipBackup    bool
}

// UpgradeCmd is `siem upgrade`.
var UpgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Move the stack to a new chart revision and/or reconciler image",
	Long: `Sets securityOperations.chartRevision (--chart-revision) and/or
reconciler.imageTag (--reconciler-tag) in the general config, then:

  1. takes a backup ('siem backup --wait'; --skip-backup leaves it out)
  2. runs 'siem apply' with the given apply flags (render, commit, --push,
     --sync, wait for Healthy)
  3. verifies: with --sync, fails unless 'siem status' is healthy

The render keeps every existing credential; only files whose content changes
are rewritten. --dry-run prints the render diff and changes nothing.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		if upgradeFlags.chartRevision == "" && upgradeFlags.reconcilerTag == "" {
			return errors.New("pass --chart-revision and/or --reconciler-tag")
		}
		if err := requireClusterDir(); err != nil {
			return err
		}
		f, err := siemctl.OpenGeneralConfig(generalConfigPath())
		if err != nil {
			return err
		}
		cfg, err := f.SecurityOperations()
		if err != nil {
			return err
		}
		if cfg == nil {
			return errors.New("cluster.securityOperations is not set")
		}
		from := fmt.Sprintf("chart %q, reconciler %q", cfg.ChartRevision, cfg.Reconciler.ImageTag)
		if upgradeFlags.chartRevision != "" {
			cfg.ChartRevision = upgradeFlags.chartRevision
		}
		if upgradeFlags.reconcilerTag != "" {
			cfg.Reconciler.ImageTag = upgradeFlags.reconcilerTag
		}
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Upgrading from %s to chart %q, reconciler %q.\n",
			from, cfg.ChartRevision, cfg.Reconciler.ImageTag)

		if flagDryRun {
			// Apply the change in memory only, then show the render diff.
			if _, err := loadConfig(); err != nil {
				return err
			}
			live := config.ParsedGeneralConfig.Cluster.SecurityOperations
			live.ChartRevision = cfg.ChartRevision
			live.Reconciler.ImageTag = cfg.Reconciler.ImageTag
			return runApply(cmd, "upgrade", "")
		}

		if !upgradeFlags.skipBackup {
			live, err := loadConfig()
			if err != nil {
				return err
			}
			backupFlags.wait = true
			if backupFlags.timeout == 0 {
				backupFlags.timeout = 2 * time.Hour
			}
			if err := runBackup(cmd, tenantCodes(live), ""); err != nil {
				return fmt.Errorf("pre-upgrade backup (pass --skip-backup to go without): %w", err)
			}
		}

		if err := f.SetSecurityOperations(cfg); err != nil {
			return err
		}
		if err := f.Save(); err != nil {
			return err
		}
		if _, err := loadConfig(); err != nil {
			return err
		}
		msg := applyFlags.message
		if msg == "" {
			msg = fmt.Sprintf("chore(security-operations): upgrade to chart %s", cfg.ChartRevision)
			if cfg.ChartRevision == "" {
				msg = "chore(security-operations): upgrade the reconciler to " + cfg.Reconciler.ImageTag
			}
		}
		return runApply(cmd, "upgrade", msg)
	},
}

func init() {
	SiemCmd.AddCommand(UpgradeCmd)
	addClusterDirFlags(UpgradeCmd)
	addSealingFlag(UpgradeCmd)
	addApplyFlags(UpgradeCmd)
	f := UpgradeCmd.Flags()
	f.StringVar(&upgradeFlags.chartRevision, "chart-revision", "", "New KubeAid revision (tag or branch) of the charts")
	f.StringVar(&upgradeFlags.reconcilerTag, "reconciler-tag", "", "New siem-reconciler image tag")
	f.BoolVar(&upgradeFlags.skipBackup, "skip-backup", false, "Do not take a backup first")
	f.StringVar(&backupFlags.veleroSchedule, "velero-schedule", "", "Velero Schedule whose template the pre-upgrade Backup copies")
	f.BoolVar(&backupFlags.skipVelero, "skip-velero", false, "Leave the Velero Backup out of the pre-upgrade backup")
}
