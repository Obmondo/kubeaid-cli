// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package siem

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/siemctl"
)

var initFlags struct {
	nonInteractive bool

	clusterName      string
	kubeaidURL       string
	kubeaidConfigURL string

	domain           string
	hostPrefix       string
	keycloakURL      string
	keycloakRealm    string
	agentHost        string
	agentAddress     string
	chartRevision    string
	ingressClass     string
	clusterIssuer    string
	sharedStorage    string
	reconciler       bool
	reconcilerTag    string
	reconcilerDryRun bool
	aiTriage         bool
	tenants          []string
}

// InitCmd is `siem init`.
var InitCmd = &cobra.Command{
	Use:   "init",
	Short: "Write or update cluster.securityOperations in the cluster's general config",
	Long: `Creates or updates the securityOperations block of the general config
(default: ` + generalConfigFileName + ` in --cluster-dir). Flags set fields; on a
terminal, required fields that are still empty are asked for (--non-interactive
turns that off). Everything else in the file, comments included, is kept; the
block itself is rewritten, so comments inside it are lost.

Tenants: --tenant code=Display name (repeatable) adds tenants that are not there
yet. An all-digit code gets its agent ports from agentPortBase; use
'siem tenant add' to give other codes explicit ports.

A missing file is created with forkURLs and cluster.name (--kubeaid-url,
--kubeaid-config-url, --cluster-name), enough for 'siem render' and 'siem apply'.`,
	Args: cobra.NoArgs,
	RunE: runInit,
}

func runInit(cmd *cobra.Command, _ []string) error {
	if err := os.MkdirAll(clusterDir, 0o700); err != nil {
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
		cfg = &config.SecurityOperationsConfig{}
	}
	cfg.Enabled = true
	flags := cmd.Flags()

	set := func(name string, dst *string, v string) {
		if flags.Changed(name) {
			*dst = v
		}
	}
	set("domain", &cfg.Domain, initFlags.domain)
	set("host-prefix", &cfg.HostPrefix, initFlags.hostPrefix)
	set("keycloak-url", &cfg.Keycloak.URL, initFlags.keycloakURL)
	set("keycloak-realm", &cfg.Keycloak.Realm, initFlags.keycloakRealm)
	set("agent-host", &cfg.AgentHost, initFlags.agentHost)
	set("agent-address", &cfg.AgentAddress, initFlags.agentAddress)
	set("chart-revision", &cfg.ChartRevision, initFlags.chartRevision)
	set("ingress-class", &cfg.IngressClassName, initFlags.ingressClass)
	set("cluster-issuer", &cfg.ClusterIssuer, initFlags.clusterIssuer)
	set("shared-storage-class", &cfg.SharedStorageClass, initFlags.sharedStorage)
	set("reconciler-tag", &cfg.Reconciler.ImageTag, initFlags.reconcilerTag)
	if flags.Changed("reconciler") {
		cfg.Reconciler.Enabled = initFlags.reconciler
	}
	if flags.Changed("reconciler-dry-run") {
		v := initFlags.reconcilerDryRun
		cfg.Reconciler.DryRun = &v
	}
	if flags.Changed("ai-triage") {
		cfg.AITriage.Enabled = initFlags.aiTriage
	}
	for _, spec := range initFlags.tenants {
		code, name, ok := strings.Cut(spec, "=")
		if !ok {
			return fmt.Errorf("--tenant %q: want code=Display name", spec)
		}
		if tenantExists(cfg, code) {
			continue
		}
		if err := siemctl.AddTenant(cfg, config.SecurityOperationsTenant{Code: code, Name: name}); err != nil {
			return err
		}
	}

	top := struct{ name, kubeaid, kubeaidConfig string }{
		f.GetString("cluster", "name"), f.GetString("forkURLs", "kubeaid", "url"),
		f.GetString("forkURLs", "kubeaidConfig", "url"),
	}
	set("cluster-name", &top.name, initFlags.clusterName)
	set("kubeaid-url", &top.kubeaid, initFlags.kubeaidURL)
	set("kubeaid-config-url", &top.kubeaidConfig, initFlags.kubeaidConfigURL)

	interactive := !initFlags.nonInteractive && term.IsTerminal(int(os.Stdin.Fd()))
	if interactive {
		if err := promptMissing(cfg, &top.name, &top.kubeaid, &top.kubeaidConfig); err != nil {
			return err
		}
	}

	var missing []string
	for name, v := range map[string]string{
		"cluster name (--cluster-name)":                     top.name,
		"forkURLs.kubeaid.url (--kubeaid-url)":              top.kubeaid,
		"forkURLs.kubeaidConfig.url (--kubeaid-config-url)": top.kubeaidConfig,
		"domain (--domain)":                                 cfg.Domain,
		"agentHost (--agent-host)":                          cfg.AgentHost,
		"agentAddress (--agent-address)":                    cfg.AgentAddress,
	} {
		if v == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing: %s", strings.Join(missing, ", "))
	}

	f.SetString(top.name, "cluster", "name")
	f.SetString(top.kubeaid, "forkURLs", "kubeaid", "url")
	f.SetString(top.kubeaidConfig, "forkURLs", "kubeaidConfig", "url")
	if err := f.SetSecurityOperations(cfg); err != nil {
		return err
	}

	if flagDryRun {
		out, err := f.Bytes()
		if err != nil {
			return err
		}
		_, err = cmd.OutOrStdout().Write(out)
		return err
	}
	if err := f.Save(); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Wrote cluster.securityOperations to %s (%d tenants).\n"+
		"Next: kubeaid-cli siem preflight --cluster-dir %s, then kubeaid-cli siem apply --cluster-dir %s --dry-run\n",
		f.Path, len(cfg.Tenants), clusterDir, clusterDir)
	return nil
}

func tenantExists(cfg *config.SecurityOperationsConfig, code string) bool {
	for _, t := range cfg.Tenants {
		if t.Code == code {
			return true
		}
	}
	return false
}

func promptMissing(cfg *config.SecurityOperationsConfig, name, kubeaid, kubeaidConfig *string) error {
	required := func(s string) error {
		if strings.TrimSpace(s) == "" {
			return errors.New("required")
		}
		return nil
	}
	var fields []huh.Field
	input := func(title, desc string, v *string) {
		if *v == "" {
			fields = append(fields, huh.NewInput().Title(title).Description(desc).Value(v).Validate(required))
		}
	}
	input("Cluster name:", "cluster.name", name)
	input("KubeAid repository URL:", "forkURLs.kubeaid.url, e.g. https://github.com/Obmondo/KubeAid", kubeaid)
	input("kubeaid-config repository URL:", "forkURLs.kubeaidConfig.url", kubeaidConfig)
	input("SOC base domain:", "Host names are <prefix><component>.<domain>", &cfg.Domain)
	input("Keycloak URL:", "Root URL including /auth, e.g. https://keycloak.example.com/auth", &cfg.Keycloak.URL)
	input("Agent host name:", "Public host name the Wazuh agents connect to", &cfg.AgentHost)
	input("Agent address:", "External IP the per-tenant agent Services listen on", &cfg.AgentAddress)
	if len(fields) == 0 {
		return nil
	}
	return huh.NewForm(huh.NewGroup(fields...).Title("Security operations")).Run()
}

func init() {
	SiemCmd.AddCommand(InitCmd)
	addClusterDirFlags(InitCmd)
	addDryRunFlag(InitCmd, "Print the resulting general config instead of writing it")
	f := InitCmd.Flags()
	f.BoolVar(&initFlags.nonInteractive, "non-interactive", false, "Never prompt; fail when a required field is missing")
	f.StringVar(&initFlags.clusterName, "cluster-name", "", "cluster.name (new file only, or to change it)")
	f.StringVar(&initFlags.kubeaidURL, "kubeaid-url", "", "forkURLs.kubeaid.url")
	f.StringVar(&initFlags.kubeaidConfigURL, "kubeaid-config-url", "", "forkURLs.kubeaidConfig.url")
	f.StringVar(&initFlags.domain, "domain", "", "Base domain of every SOC host name")
	f.StringVar(&initFlags.hostPrefix, "host-prefix", "", "Prefix of every SOC host name")
	f.StringVar(&initFlags.keycloakURL, "keycloak-url", "", "Keycloak root URL including any context path")
	f.StringVar(&initFlags.keycloakRealm, "keycloak-realm", "", "Keycloak realm (default soc)")
	f.StringVar(&initFlags.agentHost, "agent-host", "", "Public host name the Wazuh agents dial")
	f.StringVar(&initFlags.agentAddress, "agent-address", "", "External IP of the per-tenant agent Services")
	f.StringVar(&initFlags.chartRevision, "chart-revision", "", "KubeAid revision of the charts (default: forkURLs.kubeaid.version)")
	f.StringVar(&initFlags.ingressClass, "ingress-class", "", "IngressClass of the SOC Ingresses (default traefik)")
	f.StringVar(&initFlags.clusterIssuer, "cluster-issuer", "", "cert-manager ClusterIssuer (default letsencrypt-prod)")
	f.StringVar(&initFlags.sharedStorage, "shared-storage-class", "", "ReadWriteMany StorageClass for IRIS")
	f.BoolVar(&initFlags.reconciler, "reconciler", false, "Enable the siem-reconciler CronJob")
	f.StringVar(&initFlags.reconcilerTag, "reconciler-tag", "", "siem-reconciler image tag")
	f.BoolVar(&initFlags.reconcilerDryRun, "reconciler-dry-run", true, "Reconciler only prints its plan")
	f.BoolVar(&initFlags.aiTriage, "ai-triage", false, "Enable IRIS AI triage with the in-cluster model")
	f.StringArrayVar(&initFlags.tenants, "tenant", nil, "Tenant to add, code=Display name (repeatable)")
}
