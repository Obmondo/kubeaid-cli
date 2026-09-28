// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package siem

import (
	"context"
	"errors"
	"io"

	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/siemctl"
)

var statusFlags struct {
	agents bool
	strict bool
}

// StatusCmd is `siem status`.
var StatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show the state of the security operations stack (read-only)",
	Long: `Reads, and changes nothing:

  - the Argo CD Applications (root, sealed-secrets, security-operations,
    wazuh-<code>): sync and health status and revision
  - the pods of every component, per namespace (ready/total)
  - the reconciler: its /status endpoint when it runs long-running (Service
    siem-reconciler-metrics through the API server's service proxy, with the
    content package version when it reports one), else the last CronJob Job,
    its result, finish time and its "N changes" line
  - per tenant: the Wazuh indexer's readiness, whether the enrolment-bundle
    Secret has every key (key names only; values are never read out), and the
    number of connected agents (agent_control -l in the tenant's manager
    through pods/exec; --agents=false skips it)

--strict exits non-zero when anything is not Synced/Healthy/ready.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		return printStatus(cmd.Context(), cmd.OutOrStdout(), cfg, statusFlags.strict)
	},
}

// printStatus prints the stack's state. The reconciler's result comes from its
// /status endpoint when it runs long-running (Service siem-reconciler-metrics),
// and from the last CronJob Job and its pod log otherwise.
func printStatus(ctx context.Context, out io.Writer, cfg *config.SecurityOperationsConfig, strict bool) error {
	c, err := newClients(ctx)
	if err != nil {
		return err
	}
	opts := siemctl.StatusOptions{
		Kube:                     c.kube,
		Dynamic:                  c.dynamic,
		Tenants:                  tenantCodes(cfg),
		Logs:                     c.podLogs,
		ReconcilerStatusEndpoint: c.reconcilerStatus,
	}
	if statusFlags.agents {
		opts.Agents = c.connectedAgents
	}
	st, err := siemctl.CollectStatus(ctx, opts)
	if err != nil {
		return err
	}
	if err := st.Print(out); err != nil {
		return err
	}
	if strict && !st.Healthy() {
		return errors.New("the stack is not healthy")
	}
	return nil
}

func init() {
	SiemCmd.AddCommand(StatusCmd)
	addClusterDirFlags(StatusCmd)
	StatusCmd.Flags().BoolVar(&statusFlags.agents, "agents", true,
		"Count connected agents in each tenant manager (pods/exec of agent_control -l)")
	StatusCmd.Flags().BoolVar(&statusFlags.strict, "strict", false, "Exit non-zero unless everything is healthy")
}
