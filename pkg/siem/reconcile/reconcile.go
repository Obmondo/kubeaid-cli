// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

// Package reconcile runs the SIEM reconciler components in order
// (secrets, enrolment, keycloak, iris, wazuh, wazuhcentral,
// velociraptor) and collects their per-object results. A failing
// component (or Wazuh manager) is reported and the run continues with
// the next one.
package reconcile

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"k8s.io/client-go/kubernetes"

	"github.com/Obmondo/kubeaid-cli/pkg/siem/config"
	"github.com/Obmondo/kubeaid-cli/pkg/siem/enrolment"
	"github.com/Obmondo/kubeaid-cli/pkg/siem/httpx"
	"github.com/Obmondo/kubeaid-cli/pkg/siem/iris"
	"github.com/Obmondo/kubeaid-cli/pkg/siem/report"
	"github.com/Obmondo/kubeaid-cli/pkg/siem/secrets"
	"github.com/Obmondo/kubeaid-cli/pkg/siem/velociraptor"
	"github.com/Obmondo/kubeaid-cli/pkg/siem/wazuh"
	"github.com/Obmondo/kubeaid-cli/pkg/siem/wazuhcentral"
)

// Component names, in run order. Wazuh results carry one component
// per manager ("wazuh/<code>"); --only wazuh selects all of them.
const (
	ComponentSecrets      = secrets.Component
	ComponentEnrolment    = enrolment.Component
	ComponentKeycloak     = "keycloak"
	ComponentIRIS         = iris.Component
	ComponentWazuh        = wazuh.Component
	ComponentWazuhCentral = wazuhcentral.Component
	ComponentVelociraptor = velociraptor.Component
)

// Components lists every component in run order.
var Components = []string{
	ComponentSecrets, ComponentEnrolment, ComponentKeycloak, ComponentIRIS,
	ComponentWazuh, ComponentWazuhCentral, ComponentVelociraptor,
}

// Options configure a run.
type Options struct {
	Config *config.Config
	Kube   kubernetes.Interface
	DryRun bool
	// Only limits the run to these components; empty means all.
	Only []string
}

// ParseOnly validates a comma-separated --only value.
func ParseOnly(v string) ([]string, error) {
	if strings.TrimSpace(v) == "" {
		return nil, nil
	}
	valid := map[string]bool{}
	for _, c := range Components {
		valid[c] = true
	}
	var out []string
	for _, part := range strings.Split(v, ",") {
		p := strings.TrimSpace(part)
		if !valid[p] {
			return nil, fmt.Errorf("unknown component %q (want one of %s)", p, strings.Join(Components, ","))
		}
		out = append(out, p)
	}
	return out, nil
}

// Run reconciles the selected components and returns all results.
func Run(ctx context.Context, opts Options) []report.Result {
	only := map[string]bool{}
	for _, c := range opts.Only {
		only[c] = true
	}
	selected := func(c string) bool { return len(only) == 0 || only[c] }
	store := secrets.Store{Kube: opts.Kube}
	cfg := opts.Config

	var results []report.Result
	if selected(ComponentSecrets) {
		results = append(results, secrets.Ensure(ctx, opts.Kube, cfg.Secrets, opts.DryRun)...)
		results = append(results, secrets.Copy(ctx, opts.Kube, cfg.SecretCopies, cfg.Secrets, opts.DryRun)...)
	}
	if selected(ComponentEnrolment) {
		results = append(results, enrolment.Ensure(ctx, opts.Kube, cfg.Enrolment, opts.DryRun)...)
	}
	if selected(ComponentKeycloak) {
		results = append(results, runKeycloak(ctx, cfg, store, opts.DryRun)...)
	}
	if selected(ComponentIRIS) && cfg.Components.IRIS != nil {
		results = append(results, runIRIS(ctx, cfg, store, opts.DryRun)...)
	}
	if selected(ComponentWazuh) {
		for _, w := range cfg.Components.Wazuh {
			results = append(results, runWazuh(ctx, cfg, w, store, opts.DryRun)...)
		}
	}
	if selected(ComponentWazuhCentral) && cfg.Components.WazuhCentral != nil {
		results = append(results, runWazuhCentral(ctx, cfg, opts.Kube, store, opts.DryRun)...)
	}
	if selected(ComponentVelociraptor) && cfg.Components.Velociraptor != nil {
		results = append(results, runVelociraptor(ctx, cfg, store, opts.DryRun)...)
	}
	return results
}

func errorResult(component, name string, err error) []report.Result {
	return []report.Result{{Component: component, Kind: "setup", Name: name, Action: report.ActionError, Detail: err.Error()}}
}

func runIRIS(ctx context.Context, cfg *config.Config, store secrets.Store, dryRun bool) []report.Result {
	ic := cfg.Components.IRIS
	key, err := store.Read(ctx, ic.APIKeySecretRef)
	if err != nil {
		return errorResult(ComponentIRIS, "api-key", err)
	}
	hc, err := httpx.Client(ic.CAFile, ic.InsecureSkipVerify)
	if err != nil {
		return errorResult(ComponentIRIS, "http", err)
	}
	return iris.Reconcile(ctx, &iris.Client{BaseURL: ic.URL, APIKey: key, HTTP: hc}, IRISSpec(cfg), dryRun)
}

// IRISSpec derives the desired IRIS state from the config.
func IRISSpec(cfg *config.Config) iris.Spec {
	ic := cfg.Components.IRIS
	spec := iris.Spec{InitialCustomer: ic.InitialCustomer}
	for _, t := range cfg.Tenants {
		spec.Customers = append(spec.Customers, iris.Customer{Name: t.Name, Description: "Tenant " + t.Code})
	}
	for _, sa := range ic.ServiceAccounts {
		spec.ServiceAccounts = append(spec.ServiceAccounts, iris.ServiceAccount{Login: sa.Login, Groups: sa.Groups})
	}
	return spec
}

func runWazuh(ctx context.Context, cfg *config.Config, wc config.Wazuh, store secrets.Store, dryRun bool) []report.Result {
	component := wazuh.ComponentName(wc.Tenant)
	user, pass, err := readCreds(ctx, store, wc.CredSecretRef)
	if err != nil {
		return errorResult(component, "credentials", err)
	}
	hc, err := httpx.Client(wc.CAFile, wc.InsecureSkipVerify)
	if err != nil {
		return errorResult(component, "http", err)
	}
	client := &wazuh.Client{BaseURL: wc.URL, Username: user, Password: pass, HTTP: hc}
	return wazuh.Reconcile(ctx, client, WazuhSpec(cfg, wc), dryRun)
}

// WazuhSpec derives the desired API RBAC of one tenant's manager: the
// operator roles and the tenant group, each mapped through a rule.
func WazuhSpec(cfg *config.Config, wc config.Wazuh) wazuh.Spec {
	spec := wazuh.Spec{Tenant: wc.Tenant}
	mapTo := func(backendRole, apiRole string) {
		if backendRole != "" {
			spec.Mappings = append(spec.Mappings, wazuh.RoleMapping{Rule: wazuh.RuleName(backendRole), BackendRole: backendRole, APIRole: apiRole})
		}
	}
	mapTo(cfg.Operators.AdminRole, wc.AdminAPIRole)
	mapTo(cfg.Operators.AnalystRole, wc.AnalystAPIRole)
	mapTo(cfg.GroupName(config.Tenant{Code: wc.Tenant}), wc.TenantAPIRole)
	return spec
}

func runWazuhCentral(ctx context.Context, cfg *config.Config, kube kubernetes.Interface, store secrets.Store, dryRun bool) []report.Result {
	wc := cfg.Components.WazuhCentral
	var results []report.Result
	if d := wc.DashboardConfigSecret; d != nil {
		results = append(results, wazuhcentral.EnsureDashboardConfig(ctx, kube, *d, cfg.Components.Wazuh, dryRun)...)
	}
	if len(wc.Remotes) == 0 {
		return results
	}
	user, pass, err := readCreds(ctx, store, wc.CredSecretRef)
	if err != nil {
		return append(results, errorResult(ComponentWazuhCentral, "credentials", err)...)
	}
	hc, err := httpx.Client(wc.CAFile, wc.InsecureSkipVerify)
	if err != nil {
		return append(results, errorResult(ComponentWazuhCentral, "http", err)...)
	}
	client := &wazuhcentral.Client{BaseURL: wc.URL, Username: user, Password: pass, HTTP: hc}
	return append(results, wazuhcentral.Reconcile(ctx, client, WazuhCentralSpec(cfg), dryRun)...)
}

// WazuhCentralSpec derives the desired cross-cluster search remotes.
func WazuhCentralSpec(cfg *config.Config) wazuhcentral.Spec {
	var spec wazuhcentral.Spec
	for _, r := range cfg.Components.WazuhCentral.Remotes {
		spec.Remotes = append(spec.Remotes, wazuhcentral.Remote{Alias: r.Alias, Seeds: r.Seeds})
	}
	return spec
}

// readCreds reads a username/password pair.
func readCreds(ctx context.Context, store secrets.Store, ref config.WazuhCredsRef) (string, string, error) {
	user, err := store.Read(ctx, config.SecretRef{Namespace: ref.Namespace, Name: ref.Name, Key: ref.UsernameKey})
	if err != nil {
		return "", "", err
	}
	pass, err := store.Read(ctx, config.SecretRef{Namespace: ref.Namespace, Name: ref.Name, Key: ref.PasswordKey})
	if err != nil {
		return "", "", err
	}
	return user, pass, nil
}

func runVelociraptor(ctx context.Context, cfg *config.Config, store secrets.Store, dryRun bool) []report.Result {
	vc := cfg.Components.Velociraptor
	var raw []byte
	if vc.APIClientFile != "" {
		b, err := os.ReadFile(vc.APIClientFile)
		if err != nil {
			return errorResult(ComponentVelociraptor, "api-client", err)
		}
		raw = b
	} else {
		v, err := store.Read(ctx, *vc.APIClientSecretRef)
		if err != nil {
			return errorResult(ComponentVelociraptor, "api-client", err)
		}
		raw = []byte(v)
	}
	apiCfg, err := velociraptor.ParseAPIClient(raw)
	if err != nil {
		return errorResult(ComponentVelociraptor, "api-client", err)
	}
	client, err := velociraptor.Dial(apiCfg, vc.Address)
	if err != nil {
		return errorResult(ComponentVelociraptor, "connect", err)
	}
	defer func() { _ = client.Close() }()
	return velociraptor.Reconcile(ctx, client, VelociraptorSpec(cfg), dryRun)
}

// VelociraptorSpec derives the desired Velociraptor state.
func VelociraptorSpec(cfg *config.Config) velociraptor.Spec {
	var spec velociraptor.Spec
	for _, t := range cfg.Tenants {
		spec.Orgs = append(spec.Orgs, t.Name)
	}
	for _, m := range cfg.Components.Velociraptor.ServerMonitoring {
		spec.Monitoring = append(spec.Monitoring, velociraptor.MonitoredArtifact{Artifact: m.Artifact, Parameters: m.Parameters})
	}
	return spec
}

// generatedKeys indexes the Secrets the secrets component will create,
// so a dry run can tell "missing because not created yet" from
// "missing by mistake".
func generatedKeys(cfg *config.Config) map[string]bool {
	out := map[string]bool{}
	for _, s := range cfg.Secrets {
		for _, k := range s.Keys {
			out[s.Namespace+"/"+s.Name+"/"+k.Key] = true
		}
	}
	return out
}

func refKey(r config.SecretRef) string { return r.Namespace + "/" + r.Name + "/" + r.Key }

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}
