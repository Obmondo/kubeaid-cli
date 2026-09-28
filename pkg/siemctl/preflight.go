// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package siemctl

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/url"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	coreV1 "k8s.io/api/core/v1"
	apiErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
)

// CheckStatus is the outcome of one preflight check.
type CheckStatus string

// Check outcomes. A warning does not stop apply unless --strict is given.
const (
	CheckPass CheckStatus = "ok"
	CheckWarn CheckStatus = "warn"
	CheckFail CheckStatus = "FAIL"
)

// Check is one preflight result. Detail never carries secret values.
type Check struct {
	Name   string
	Status CheckStatus
	Detail string
}

// Profiles for the node capacity check.
const (
	ProfileSingle   = "single"
	ProfileStandard = "standard"
)

// PreflightOptions are the inputs of RunPreflight.
type PreflightOptions struct {
	Kube    kubernetes.Interface
	Dynamic dynamic.Interface
	// Config is cluster.securityOperations with the parser's defaults.
	Config *config.SecurityOperationsConfig
	// Profile sizes the capacity check: single or standard (default).
	Profile string
	// RequireVelero makes a missing Velero a failure instead of a warning.
	RequireVelero bool
	// SkipDNS skips the host name resolution check.
	SkipDNS bool
	// LookupHost resolves a host name; nil means net.DefaultResolver.
	LookupHost func(ctx context.Context, host string) ([]string, error)
}

// kindSealedSecret is the sealed-secrets CRD kind, and sealedSecretsName the
// controller's (and its Argo CD App's) name.
const (
	kindSealedSecret  = "SealedSecret"
	sealedSecretsName = "sealed-secrets"
)

// Names of the checks that are reported from more than one place.
const (
	checkClusterIssuerName = "cluster issuer"
	checkCapacityName      = "node capacity"
)

// veleroPrerequisite is the name of the Velero prerequisite: the only optional
// one, and --require-velero makes it required.
const veleroPrerequisite = "velero"

// Prerequisite is a CRD-backed controller the stack needs.
type Prerequisite struct {
	Name         string
	GroupVersion string
	Kind         string
	// PodSelector finds the controller's pods in any namespace.
	PodSelector string
	// Optional prerequisites only warn when missing.
	Optional bool
}

// Prerequisites are the controllers the SOC charts rely on: Argo CD deploys
// them, sealed-secrets unseals the Wazuh credentials, cert-manager issues the
// Ingress and Wazuh certificates, CloudNativePG runs IRIS' PostgreSQL,
// mariadb-operator runs MISP's MariaDB, and Velero takes the backups.
var Prerequisites = []Prerequisite{
	{
		Name: "argo-cd", GroupVersion: "argoproj.io/" + versionV1Alpha1, Kind: kindApplication,
		PodSelector: "app.kubernetes.io/name=argocd-application-controller",
	},
	{
		Name: sealedSecretsName, GroupVersion: "bitnami.com/" + versionV1Alpha1, Kind: kindSealedSecret,
		PodSelector: "app.kubernetes.io/name=" + sealedSecretsName,
	},
	{
		Name: "cert-manager", GroupVersion: "cert-manager.io/v1", Kind: "Certificate",
		PodSelector: "app.kubernetes.io/name=cert-manager",
	},
	{
		Name: "cloudnative-pg", GroupVersion: groupCNPG + "/v1", Kind: "Cluster",
		PodSelector: "app.kubernetes.io/name=cloudnative-pg",
	},
	{
		Name: "mariadb-operator", GroupVersion: groupMariaDB + "/" + versionV1Alpha1, Kind: "MariaDB",
		PodSelector: "app.kubernetes.io/name=mariadb-operator",
	},
	{
		Name: veleroPrerequisite, GroupVersion: groupVelero + "/v1", Kind: "Backup",
		PodSelector: "app.kubernetes.io/name=velero", Optional: true,
	},
}

// Capacity is a rough CPU (cores) and memory (GiB) need.
type Capacity struct {
	CPU    float64
	Memory float64
}

// RequiredCapacity estimates what the stack requests: the central
// components, each tenant's Wazuh (per indexer replica) and Ollama when AI
// triage is on. Rough numbers from the chart defaults; the single profile is
// the reduced quickstart sizing.
func RequiredCapacity(cfg *config.SecurityOperationsConfig, profile string) Capacity {
	central, perReplica, ai := Capacity{8, 24}, Capacity{2, 6}, Capacity{4, 10}
	if profile == ProfileSingle {
		central, perReplica, ai = Capacity{3, 8}, Capacity{1, 3}, Capacity{2, 8}
	}
	need := central
	for _, t := range cfg.Tenants {
		replicas := t.IndexerReplicas
		if replicas < 1 {
			replicas = 1
		}
		need.CPU += perReplica.CPU * float64(replicas)
		need.Memory += perReplica.Memory * float64(replicas)
	}
	if cfg.AITriage.Enabled {
		need.CPU += ai.CPU
		need.Memory += ai.Memory
	}
	return need
}

// RunPreflight runs every check. It only reads from the cluster.
func RunPreflight(ctx context.Context, opts PreflightOptions) []Check {
	var checks []Check

	version, err := opts.Kube.Discovery().ServerVersion()
	if err != nil {
		return append(checks, Check{"cluster reachable", CheckFail, err.Error()})
	}
	checks = append(checks, Check{"cluster reachable", CheckPass, "Kubernetes " + version.GitVersion})

	for _, p := range Prerequisites {
		checks = append(checks, checkPrerequisite(ctx, opts, p))
	}

	cfg := opts.Config
	if cfg != nil && opts.Dynamic != nil && cfg.ClusterIssuer != "" {
		checks = append(checks, checkClusterIssuer(ctx, opts.Dynamic, cfg.ClusterIssuer))
	}

	checks = append(checks, checkStorageClasses(ctx, opts.Kube, cfg)...)

	if cfg != nil && !opts.SkipDNS {
		checks = append(checks, checkDNS(ctx, opts, ConfiguredHosts(cfg))...)
	}

	if cfg != nil {
		checks = append(checks, checkCapacity(ctx, opts.Kube, RequiredCapacity(cfg, opts.Profile)))
	}
	return checks
}

func checkPrerequisite(ctx context.Context, opts PreflightOptions, p Prerequisite) Check {
	name := "controller " + p.Name
	missing := CheckFail
	if p.Optional && (p.Name != veleroPrerequisite || !opts.RequireVelero) {
		missing = CheckWarn
	}

	resources, err := opts.Kube.Discovery().ServerResourcesForGroupVersion(p.GroupVersion)
	found := false
	if err == nil {
		for _, r := range resources.APIResources {
			if r.Kind == p.Kind {
				found = true
				break
			}
		}
	}
	if !found {
		return Check{name, missing, fmt.Sprintf("CRD %s %s is not installed", p.GroupVersion, p.Kind)}
	}

	pods, err := opts.Kube.CoreV1().Pods("").List(ctx, metaV1.ListOptions{LabelSelector: p.PodSelector})
	if err != nil {
		return Check{name, CheckWarn, "CRD present; listing controller pods: " + err.Error()}
	}
	running := 0
	for _, pod := range pods.Items {
		if pod.Status.Phase == coreV1.PodRunning {
			running++
		}
	}
	if running == 0 {
		return Check{name, CheckWarn, fmt.Sprintf("CRD present, but no running pod matches %s", p.PodSelector)}
	}
	return Check{name, CheckPass, fmt.Sprintf("CRD present, %d pod(s) running", running)}
}

var clusterIssuerGVR = schema.GroupVersionResource{Group: "cert-manager.io", Version: "v1", Resource: "clusterissuers"}

func checkClusterIssuer(ctx context.Context, dyn dynamic.Interface, name string) Check {
	_, err := dyn.Resource(clusterIssuerGVR).Get(ctx, name, metaV1.GetOptions{})
	switch {
	case err == nil:
		return Check{checkClusterIssuerName, CheckPass, name}
	case apiErrors.IsNotFound(err):
		return Check{checkClusterIssuerName, CheckFail, fmt.Sprintf("ClusterIssuer %q does not exist", name)}
	default:
		return Check{checkClusterIssuerName, CheckWarn, err.Error()}
	}
}

func checkStorageClasses(ctx context.Context, kube kubernetes.Interface, cfg *config.SecurityOperationsConfig) []Check {
	list, err := kube.StorageV1().StorageClasses().List(ctx, metaV1.ListOptions{})
	if err != nil {
		return []Check{{"storage classes", CheckFail, err.Error()}}
	}
	var checks []Check
	var defaults []string
	names := map[string]bool{}
	for _, sc := range list.Items {
		names[sc.Name] = true
		if sc.Annotations["storageclass.kubernetes.io/is-default-class"] == "true" {
			defaults = append(defaults, sc.Name)
		}
	}
	if len(defaults) == 0 {
		checks = append(checks, Check{
			"default storage class", CheckFail,
			"no StorageClass is marked default; the SOC volumes use the default class",
		})
	} else {
		checks = append(checks, Check{"default storage class", CheckPass, strings.Join(defaults, ",")})
	}
	if cfg != nil && cfg.SharedStorageClass != "" {
		if names[cfg.SharedStorageClass] {
			checks = append(checks, Check{"shared (RWX) storage class", CheckPass, cfg.SharedStorageClass})
		} else {
			checks = append(checks, Check{
				"shared (RWX) storage class", CheckFail,
				fmt.Sprintf("StorageClass %q (sharedStorageClass) does not exist", cfg.SharedStorageClass),
			})
		}
	}
	return checks
}

// ConfiguredHosts lists every host name cluster.securityOperations uses:
// the central hosts, each tenant dashboard, the agent host and Keycloak.
func ConfiguredHosts(cfg *config.SecurityOperationsConfig) []string {
	host := func(name string) string { return fmt.Sprintf("%s%s.%s", cfg.HostPrefix, name, cfg.Domain) }
	hosts := []string{host("wazuh"), host("iris"), host("misp"), host("velociraptor")}
	for _, t := range cfg.Tenants {
		hosts = append(hosts, host(constants.SecurityOperationsTenantNamespacePrefix+t.Code))
	}
	if cfg.AgentHost != "" {
		hosts = append(hosts, cfg.AgentHost)
	}
	if u, err := url.Parse(cfg.Keycloak.URL); err == nil && u.Hostname() != "" {
		hosts = append(hosts, u.Hostname())
	}
	return hosts
}

func checkDNS(ctx context.Context, opts PreflightOptions, hosts []string) []Check {
	lookup := opts.LookupHost
	if lookup == nil {
		lookup = net.DefaultResolver.LookupHost
	}
	var unresolved []string
	for _, h := range hosts {
		lctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		addrs, err := lookup(lctx, h)
		cancel()
		if err != nil || len(addrs) == 0 {
			unresolved = append(unresolved, h)
		}
	}
	if len(unresolved) > 0 {
		// A warning: external-dns or the operator often creates the records
		// once the Ingresses exist.
		return []Check{{"DNS", CheckWarn, "does not resolve: " + strings.Join(unresolved, ", ")}}
	}
	return []Check{{"DNS", CheckPass, fmt.Sprintf("%d host names resolve", len(hosts))}}
}

func checkCapacity(ctx context.Context, kube kubernetes.Interface, need Capacity) Check {
	nodes, err := kube.CoreV1().Nodes().List(ctx, metaV1.ListOptions{})
	if err != nil {
		return Check{checkCapacityName, CheckWarn, err.Error()}
	}
	cpu := resource.Quantity{}
	mem := resource.Quantity{}
	schedulable := 0
	for _, n := range nodes.Items {
		if n.Spec.Unschedulable || hasNoScheduleTaint(n) {
			continue
		}
		schedulable++
		cpu.Add(n.Status.Allocatable[coreV1.ResourceCPU])
		mem.Add(n.Status.Allocatable[coreV1.ResourceMemory])
	}
	haveCPU := float64(cpu.MilliValue()) / 1000
	haveMem := float64(mem.Value()) / (1 << 30)
	detail := fmt.Sprintf("%d schedulable node(s): %.1f CPU / %.1f GiB allocatable, stack needs about %.1f CPU / %.1f GiB",
		schedulable, haveCPU, haveMem, need.CPU, need.Memory)
	if haveCPU < need.CPU || haveMem < need.Memory {
		return Check{checkCapacityName, CheckWarn, detail}
	}
	return Check{checkCapacityName, CheckPass, detail}
}

func hasNoScheduleTaint(n coreV1.Node) bool {
	for _, t := range n.Spec.Taints {
		if t.Effect == coreV1.TaintEffectNoSchedule || t.Effect == coreV1.TaintEffectNoExecute {
			return true
		}
	}
	return false
}

// PreflightFailed reports whether any check failed (or warned, when strict).
func PreflightFailed(checks []Check, strict bool) bool {
	for _, c := range checks {
		if c.Status == CheckFail || (strict && c.Status == CheckWarn) {
			return true
		}
	}
	return false
}

// PrintChecks writes the checks as a table, failures last.
func PrintChecks(w io.Writer, checks []Check) error {
	sorted := append([]Check(nil), checks...)
	rank := map[CheckStatus]int{CheckPass: 0, CheckWarn: 1, CheckFail: 2}
	sort.SliceStable(sorted, func(i, j int) bool { return rank[sorted[i].Status] < rank[sorted[j].Status] })
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "CHECK\tRESULT\tDETAIL")
	for _, c := range sorted {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\n", c.Name, c.Status, c.Detail)
	}
	return tw.Flush()
}
