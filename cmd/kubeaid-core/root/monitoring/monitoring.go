// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package monitoring

import (
	"github.com/spf13/cobra"
)

// MonitoringCmd only groups monitoring subcommands; like backup, it has no
// PersistentPreRun of its own, so most subcommands run without any parsed
// cluster config. FixScrapeNamespacesCmd is the one exception - see its own
// PersistentPreRun for why it needs one.
var MonitoringCmd = &cobra.Command{
	Use:   "monitoring",
	Short: "Inspect monitoring health of a KubeAid managed K8s cluster",
}

func init() {
	MonitoringCmd.AddCommand(CheckScrapeNamespacesCmd)
	MonitoringCmd.AddCommand(FixScrapeNamespacesCmd)
}
