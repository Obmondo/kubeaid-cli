// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

// Package root holds the siem-reconciler cobra commands.
package root

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/Obmondo/kubeaid-cli/cmd/siem-reconciler/root/version"
	"github.com/Obmondo/kubeaid-cli/pkg/siem/config"
	"github.com/Obmondo/kubeaid-cli/pkg/siem/reconcile"
	"github.com/Obmondo/kubeaid-cli/pkg/siem/report"
)

// DefaultConfigPath is where the chart mounts tenants.json.
const DefaultConfigPath = "/etc/siem/tenants.json"

// flags of the root command.
var flags struct {
	configPath  string
	dryRun      bool
	only        string
	kubeconfig  string
	kubeContext string
	timeout     time.Duration
}

// RootCmd reconciles once and exits.
var RootCmd = &cobra.Command{
	Use:   "siem-reconciler",
	Short: "Reconcile Keycloak, DFIR-IRIS, Wazuh and Velociraptor with tenants.json",
	Long: `siem-reconciler reads tenants.json and makes the SIEM components match it:
Kubernetes Secrets (create-if-missing), Keycloak realm/clients/roles/groups/MFA flow,
IRIS customers, Wazuh API RBAC and Velociraptor orgs and server monitoring.

It defaults to --dry-run=true: it prints what it would change and writes nothing.
It prints one line per object and a final "N changes" line, and exits 1 when any
object could not be reconciled (drift alone is not an error).`,
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE:          run,
}

func init() {
	f := RootCmd.Flags()
	f.StringVar(&flags.configPath, "config", DefaultConfigPath, "path to tenants.json")
	f.BoolVar(&flags.dryRun, "dry-run", true, "report changes without writing anything")
	f.StringVar(&flags.only, "only", "", "comma-separated components to run: "+joinComponents())
	f.DurationVar(&flags.timeout, "timeout", 5*time.Minute, "overall timeout")
	RootCmd.PersistentFlags().StringVar(&flags.kubeconfig, "kubeconfig", "", "kubeconfig file (default: in-cluster, then $KUBECONFIG)")
	RootCmd.PersistentFlags().StringVar(&flags.kubeContext, "context", "", "kubeconfig context")

	RootCmd.AddCommand(version.VersionCommand)
	RootCmd.AddCommand(publishCmd)
}

func joinComponents() string {
	out := ""
	for i, c := range reconcile.Components {
		if i > 0 {
			out += ","
		}
		out += c
	}
	return out
}

func run(cmd *cobra.Command, _ []string) error {
	only, err := reconcile.ParseOnly(flags.only)
	if err != nil {
		return err
	}
	cfg, err := config.Load(flags.configPath)
	if err != nil {
		return err
	}
	kube, err := kubeClient()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), flags.timeout)
	defer cancel()

	results := reconcile.Run(ctx, reconcile.Options{Config: cfg, Kube: kube, DryRun: flags.dryRun, Only: only})
	if err := report.Print(cmd.OutOrStdout(), results, flags.dryRun); err != nil {
		return err
	}
	if s := report.Summarize(results); s.Errors > 0 {
		return fmt.Errorf("%d objects could not be reconciled", s.Errors)
	}
	return nil
}

// kubeClient uses --kubeconfig/--context when given, the in-cluster
// service account otherwise, and falls back to the default kubeconfig
// loading rules outside a cluster.
func kubeClient() (kubernetes.Interface, error) {
	var (
		restCfg *rest.Config
		err     error
	)
	if flags.kubeconfig == "" && flags.kubeContext == "" {
		restCfg, err = rest.InClusterConfig()
		if err != nil && !errors.Is(err, rest.ErrNotInCluster) {
			return nil, fmt.Errorf("in-cluster config: %w", err)
		}
	}
	if restCfg == nil {
		rules := clientcmd.NewDefaultClientConfigLoadingRules()
		if flags.kubeconfig != "" {
			rules.ExplicitPath = flags.kubeconfig
		}
		overrides := &clientcmd.ConfigOverrides{CurrentContext: flags.kubeContext}
		restCfg, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides).ClientConfig()
		if err != nil {
			return nil, fmt.Errorf("loading kubeconfig: %w", err)
		}
	}
	kube, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return nil, fmt.Errorf("building Kubernetes client: %w", err)
	}
	return kube, nil
}
