// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package siem

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-cli/pkg/core"
	"github.com/Obmondo/kubeaid-cli/pkg/siemctl"
)

var quickstartFlags struct {
	dir  string
	opts siemctl.QuickstartOptions
	run  bool
}

// QuickstartCmd is `siem quickstart`.
var QuickstartCmd = &cobra.Command{
	Use:   "quickstart",
	Short: "Evaluate the stack on one machine: one demo tenant, installed with Helm",
	Long: `Single-node evaluation path, without Argo CD or a kubeaid-config repository:

  1. writes <dir>/k8s/kubesoc-quickstart/kubeaid-cli.general.yaml (kept when it
     exists): one tenant "demo", reconciler with dry-run off, agent ports
     1515/1514 on --agent-address
  2. renders it like 'siem render' (sealing needs the sealed-secrets
     controller in $KUBECONFIG, or --sealed-secrets-cert)
  3. writes <dir>/install.sh: the sealed Secrets and 'helm upgrade --install'
     of security-operations and wazuh-demo from --kubeaid-dir, with the
     single-node overrides of hack/kubesoc-quickstart/values-single-<chart>.yaml
  4. with --run, runs install.sh against $KUBECONFIG (asks first, or --yes)

KubeAid's hack/kubesoc-quickstart.sh does the whole path on a fresh machine:
k3s, the prerequisites (sealed-secrets, cert-manager, CloudNativePG,
mariadb-operator, a development Keycloak), then this command with --run.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		out := cmd.OutOrStdout()
		opts := quickstartFlags.opts
		opts.Dir = quickstartFlags.dir
		dir, path, err := siemctl.WriteQuickstartConfig(opts)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(out, "General config: %s\n", path)

		clusterDir = dir
		generalConfig = path
		if _, err := loadConfig(); err != nil {
			return err
		}
		written, err := core.RenderSecurityOperations(cmd.Context(), dir)
		if err != nil {
			return err
		}
		apps, err := siemctl.ReadApplications(dir, written)
		if err != nil {
			return err
		}
		releases, err := siemctl.HelmReleasesFromApps(apps, bundleFlags.kubeaidDir, dir)
		if err != nil {
			return err
		}
		valuesDir := filepath.Join(quickstartFlags.dir, "helm")
		script, files, err := siemctl.QuickstartScript(releases, siemctl.SyncOrder(apps), dir, bundleFlags.kubeaidDir, valuesDir)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(valuesDir, 0o700); err != nil {
			return err
		}
		for name, raw := range files {
			if err := os.WriteFile(filepath.Join(valuesDir, name), raw, 0o600); err != nil {
				return err
			}
		}
		scriptPath := filepath.Join(quickstartFlags.dir, "install.sh")
		if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil { //nolint:gosec // an operator script
			return err
		}
		_, _ = fmt.Fprintf(out, "Install script: %s\n", scriptPath)

		if !quickstartFlags.run {
			_, _ = fmt.Fprintf(out, "Review it, then run it (or pass --run).\n")
			return nil
		}
		if err := confirm(cmd, "Install the stack into the cluster of $KUBECONFIG?"); err != nil {
			return err
		}
		//nolint:gosec // G204: sh runs the install script this command just wrote.
		sh := exec.CommandContext(cmd.Context(), "sh", scriptPath)
		sh.Stdout, sh.Stderr = out, cmd.ErrOrStderr()
		return sh.Run()
	},
}

func init() {
	SiemCmd.AddCommand(QuickstartCmd)
	addSealingFlag(QuickstartCmd)
	addYesFlag(QuickstartCmd, "installing")
	f := QuickstartCmd.Flags()
	o := &quickstartFlags.opts
	f.StringVar(&quickstartFlags.dir, "dir", "kubesoc-quickstart", "Working directory for the config, rendered files and install.sh")
	f.StringVar(&bundleFlags.kubeaidDir, "kubeaid-dir", "", "KubeAid checkout with the charts")
	_ = QuickstartCmd.MarkFlagRequired("kubeaid-dir")
	f.StringVar(&o.Domain, "domain", "", "Base domain, e.g. <node IP>.nip.io")
	f.StringVar(&o.AgentAddress, "agent-address", "", "Node IP the agents connect to")
	f.StringVar(&o.KeycloakURL, "keycloak-url", "", "Keycloak root URL including /auth")
	f.StringVar(&o.ClusterIssuer, "cluster-issuer", "", "cert-manager ClusterIssuer (default letsencrypt-prod)")
	f.StringVar(&o.ChartRevision, "chart-revision", "", "KubeAid revision recorded in the config")
	f.StringVar(&o.ReconcilerTag, "reconciler-tag", "", "siem-reconciler image tag")
	f.StringVar(&o.TenantCode, "tenant", siemctl.QuickstartTenantCode, "Demo tenant code")
	f.BoolVar(&o.AITriage, "ai-triage", false, "Enable AI triage (downloads the model; needs ~10 GiB more memory)")
	f.BoolVar(&quickstartFlags.run, "run", false, "Run install.sh after writing it")
}
