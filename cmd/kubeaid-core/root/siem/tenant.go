// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package siem

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	apiErrors "k8s.io/apimachinery/pkg/api/errors"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/core"
	"github.com/Obmondo/kubeaid-cli/pkg/siemctl"
)

var tenantFlags struct {
	name             string
	registrationPort int
	eventsPort       int
	retentionDays    int
	indexerReplicas  int

	confirm         bool
	export          bool
	deleteNamespace bool
}

// TenantCmd is `siem tenant`.
var TenantCmd = &cobra.Command{
	Use:   "tenant",
	Short: "Add or remove a tenant",
}

// TenantAddCmd is `siem tenant add`.
var TenantAddCmd = &cobra.Command{
	Use:   "add <code>",
	Short: "Add a tenant to cluster.securityOperations and render its files",
	Long: `Adds the tenant to the general config and renders: its wazuh-<code>
Application and fresh sealed credentials (every other tenant keeps its own).
Review, then 'kubeaid-cli siem apply' commits, pushes and syncs it. After the
first reconciler run 'kubeaid-cli siem enroll <code>' prints the agent install
commands.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireClusterDir(); err != nil {
			return err
		}
		tenant := config.SecurityOperationsTenant{
			Code:            args[0],
			Name:            tenantFlags.name,
			RetentionDays:   tenantFlags.retentionDays,
			IndexerReplicas: tenantFlags.indexerReplicas,
		}
		if tenantFlags.registrationPort != 0 || tenantFlags.eventsPort != 0 {
			tenant.AgentPorts = &config.SecurityOperationsAgentPorts{
				Registration: tenantFlags.registrationPort,
				Events:       tenantFlags.eventsPort,
			}
		}
		return editTenantsAndRender(cmd, func(cfg *config.SecurityOperationsConfig) error {
			return siemctl.AddTenant(cfg, tenant)
		})
	},
}

// TenantRemoveCmd is `siem tenant remove`.
var TenantRemoveCmd = &cobra.Command{
	Use:   "remove <code>",
	Short: "Remove a tenant (needs --confirm)",
	Long: `Removes the tenant from the general config, deletes its sealed Secrets
(sealed-secrets/wazuh-<code>/) and renders, so its wazuh-<code> Application is
gone from the rendered files. 'kubeaid-cli siem apply' then commits and syncs.

  --export            first takes a Velero Backup of the tenant namespace
                      (kubesoc-tenant-<code>-<time>), the tenant's data export.
  --delete-namespace  also deletes the wazuh-<code> Application and namespace in
                      the cluster of $KUBECONFIG now (asks again, or --yes).

The tenant's IRIS customer and Velociraptor org are not deleted: the reconciler
has no removal flags yet, so remove them in IRIS and Velociraptor by hand.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		code := args[0]
		if !tenantFlags.confirm {
			return fmt.Errorf("removing tenant %s deletes its credentials and, with --delete-namespace, its data: pass --confirm", code)
		}
		if err := requireClusterDir(); err != nil {
			return err
		}
		ctx := cmd.Context()
		out := cmd.OutOrStdout()
		ns := constants.SecurityOperationsTenantNamespacePrefix + code

		if tenantFlags.export {
			c, err := newClients(ctx)
			if err != nil {
				return err
			}
			plan, err := siemctl.PlanBackup(ctx, siemctl.BackupOptions{
				Dynamic: c.dynamic,
				Tenants: []string{code},
				Name:    "kubesoc-tenant-" + code + "-" + time.Now().UTC().Format("20060102-150405"),
			})
			if err != nil {
				return err
			}
			// Only the tenant's namespace: drop the central namespace.
			plan.Objects = onlyNamespace(plan, ns)
			plan.Print(out)
			if !flagDryRun {
				if err := plan.Execute(ctx, c.dynamic); err != nil {
					return err
				}
				if err := waitForVeleroBackup(ctx, c, plan.Name, 2*time.Hour); err != nil {
					return fmt.Errorf("export: %w", err)
				}
			}
		}

		if err := editTenantsAndRender(cmd, func(cfg *config.SecurityOperationsConfig) error {
			_, err := siemctl.RemoveTenant(cfg, code)
			return err
		}); err != nil {
			return err
		}
		sealedDir := filepath.Join(clusterDir, "sealed-secrets", ns)
		if flagDryRun {
			_, _ = fmt.Fprintf(out, "Would delete %s\n", sealedDir)
		} else if err := os.RemoveAll(sealedDir); err != nil {
			return err
		}

		if tenantFlags.deleteNamespace {
			if flagDryRun {
				_, _ = fmt.Fprintf(out, "Would delete Application %s and namespace %s\n", ns, ns)
			} else {
				if err := confirm(cmd, fmt.Sprintf("Delete Application %s and namespace %s with all its data now?", ns, ns)); err != nil {
					return err
				}
				if err := deleteTenantLive(ctx, ns); err != nil {
					return err
				}
				_, _ = fmt.Fprintf(out, "Deleted Application %s and namespace %s.\n", ns, ns)
			}
		}
		_, _ = fmt.Fprintf(out, "Tenant %s removed from the config. Remove its IRIS customer and Velociraptor org by hand.\n"+
			"Next: kubeaid-cli siem apply --cluster-dir %s\n", code, clusterDir)
		return nil
	},
}

// editTenantsAndRender changes the tenant list in the general config, then
// reloads and renders (or, with --dry-run, shows both diffs).
func editTenantsAndRender(cmd *cobra.Command, edit func(*config.SecurityOperationsConfig) error) error {
	f, err := siemctl.OpenGeneralConfig(generalConfigPath())
	if err != nil {
		return err
	}
	cfg, err := f.SecurityOperations()
	if err != nil {
		return err
	}
	if cfg == nil {
		return errors.New("cluster.securityOperations is not set: run 'kubeaid-cli siem init' first")
	}
	if err := edit(cfg); err != nil {
		return err
	}
	if err := f.SetSecurityOperations(cfg); err != nil {
		return err
	}
	out := cmd.OutOrStdout()

	if flagDryRun {
		return printTenantDryRun(cmd, f, out)
	}

	if err := f.Save(); err != nil {
		return err
	}
	if _, err := loadConfig(); err != nil {
		return err
	}
	written, err := core.RenderSecurityOperations(cmd.Context(), clusterDir)
	printWritten(out, clusterDir, written, core.SecurityOperationsLegacySealedSecretFiles(clusterDir))
	return err
}

// printTenantDryRun prints the edited general config and the render diff it
// would produce, writing nothing outside a temporary directory.
func printTenantDryRun(cmd *cobra.Command, f *siemctl.GeneralConfigFile, out io.Writer) error {
	raw, err := f.Bytes()
	if err != nil {
		return err
	}
	scratch, err := os.MkdirTemp("", "kubesoc-tenant-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(scratch) }()

	tmp := filepath.Join(scratch, generalConfigFileName)
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	prev := generalConfig
	generalConfig = tmp
	defer func() { generalConfig = prev }()

	if _, err := loadConfig(); err != nil {
		return err
	}
	diff, _, err := siemctl.DryRunDiff(cmd.Context(), clusterDir, core.RenderSecurityOperations)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(out, "--- %s (dry run)\n%s\n%s", generalConfigPath(), raw, diff)
	return nil
}

func onlyNamespace(plan *siemctl.Plan, ns string) []siemctl.PlannedObject {
	var kept []siemctl.PlannedObject
	for _, o := range plan.Objects {
		if o.GVR == siemctl.VeleroBackupGVR {
			_ = setIncludedNamespaces(o, ns)
			kept = append(kept, o)
			continue
		}
		if o.Object.GetNamespace() == ns {
			kept = append(kept, o)
		}
	}
	return kept
}

func setIncludedNamespaces(o siemctl.PlannedObject, ns string) error {
	spec, ok := o.Object.Object["spec"].(map[string]any)
	if !ok {
		return errors.New("velero Backup without spec")
	}
	spec["includedNamespaces"] = []any{ns}
	return nil
}

func deleteTenantLive(ctx context.Context, ns string) error {
	c, err := newClients(ctx)
	if err != nil {
		return err
	}
	err = c.dynamic.Resource(siemctl.ApplicationGVR).Namespace(constants.NamespaceArgoCD).
		Delete(ctx, ns, metaV1.DeleteOptions{})
	if err != nil && !apiErrors.IsNotFound(err) {
		return fmt.Errorf("deleting Application %s: %w", ns, err)
	}
	err = c.kube.CoreV1().Namespaces().Delete(ctx, ns, metaV1.DeleteOptions{})
	if err != nil && !apiErrors.IsNotFound(err) {
		return fmt.Errorf("deleting namespace %s: %w", ns, err)
	}
	return nil
}

func init() {
	SiemCmd.AddCommand(TenantCmd)
	TenantCmd.AddCommand(TenantAddCmd, TenantRemoveCmd)

	for _, c := range []*cobra.Command{TenantAddCmd, TenantRemoveCmd} {
		addClusterDirFlags(c)
		addSealingFlag(c)
		addDryRunFlag(c, "Show the config and render diff and change nothing")
	}

	f := TenantAddCmd.Flags()
	f.StringVar(&tenantFlags.name, "name", "", "Display name (IRIS customer, Velociraptor org); unique")
	_ = TenantAddCmd.MarkFlagRequired("name")
	f.IntVar(&tenantFlags.registrationPort, "registration-port", 0, "Agent registration port (derived for an all-digit code)")
	f.IntVar(&tenantFlags.eventsPort, "events-port", 0, "Agent events port (derived for an all-digit code)")
	f.IntVar(&tenantFlags.retentionDays, "retention-days", 0, "Alert retention in days (default: the chart's)")
	f.IntVar(&tenantFlags.indexerReplicas, "indexer-replicas", 0, "Wazuh indexer replicas (default 1)")

	f = TenantRemoveCmd.Flags()
	f.BoolVar(&tenantFlags.confirm, "confirm", false, "Required: confirm the removal")
	f.BoolVar(&tenantFlags.export, "export", false, "Take a Velero Backup of the tenant namespace first")
	f.BoolVar(&tenantFlags.deleteNamespace, "delete-namespace", false, "Also delete the Application and namespace in the cluster now")
	addYesFlag(TenantRemoveCmd, "deleting the namespace")
}
