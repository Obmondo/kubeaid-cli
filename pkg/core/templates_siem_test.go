// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/kubernetes"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/templates"
)

const (
	siemAppsTmpl         = "templates/argocd-apps/templates/security-operations.yaml.tmpl"
	siemValuesTmpl       = "templates/argocd-apps/values-security-operations.yaml.tmpl"
	siemTenantValuesTmpl = "templates/argocd-apps/values-wazuh-tenant.yaml.tmpl"

	siemAgentAddress = "192.0.2.10"
	siemIssuer       = "https://keycloak.example.com/auth/realms/soc"
)

// siemTenant returns a tenant as the parser leaves it: defaults applied and
// ports derived from base 20000.
func siemTenant(n int, name string) config.SecurityOperationsTenant {
	return config.SecurityOperationsTenant{
		Code:            fmt.Sprintf("%03d", n),
		Name:            name,
		IndexerReplicas: 1,
		AgentPorts: &config.SecurityOperationsAgentPorts{
			Registration: 20000 + n*10 + 5,
			Events:       20000 + n*10 + 4,
		},
	}
}

// withSIEMConfig installs parsed general and secrets configs with the given
// tenants and returns the template values the SIEM files render from.
func withSIEMConfig(t *testing.T, tenants ...config.SecurityOperationsTenant) *TemplateValues {
	t.Helper()

	origGeneral, origSecrets := config.ParsedGeneralConfig, config.ParsedSecretsConfig
	t.Cleanup(func() {
		config.ParsedGeneralConfig = origGeneral
		config.ParsedSecretsConfig = origSecrets
	})

	dryRun := true
	config.ParsedGeneralConfig = &config.GeneralConfig{}
	config.ParsedGeneralConfig.Forks = forkTV("").ForksConfig
	config.ParsedGeneralConfig.Cluster.SecurityOperations = &config.SecurityOperationsConfig{
		Enabled:          true,
		Domain:           "example.com",
		HostPrefix:       "soc-",
		Keycloak:         config.SecurityOperationsKeycloakConfig{URL: "https://keycloak.example.com/auth/", Realm: "soc"},
		AgentHost:        "agents.example.com",
		AgentAddress:     siemAgentAddress,
		AgentPortBase:    20000,
		IngressClassName: "traefik",
		ClusterIssuer:    constants.ClusterIssuerLetsEncrypt,
		Reconciler:       config.SecurityOperationsReconcilerConfig{DryRun: &dryRun},
		Tenants:          tenants,
	}

	creds := &config.SecurityOperationsCredentials{
		Central: config.SecurityOperationsWazuhCredentials{
			IndexerPassword: "central-indexer", IndexerPasswordHash: "$2a$12$central.indexer",
			DashboardPassword: "central-dashboard", DashboardPasswordHash: "$2a$12$central.dashboard",
		},
		Tenants: map[string]config.SecurityOperationsWazuhCredentials{},
	}
	for _, tenant := range tenants {
		c := tenant.Code
		creds.Tenants[c] = config.SecurityOperationsWazuhCredentials{
			IndexerPassword: "indexer-" + c, IndexerPasswordHash: "$2a$12$indexer." + c,
			DashboardPassword: "dashboard-" + c, DashboardPasswordHash: "$2a$12$dashboard." + c,
			APIPassword: "Aa1.api" + c, AuthdPassword: "authd-" + c, ClusterKey: "key-" + c,
		}
	}
	config.ParsedSecretsConfig = &config.SecretsConfig{SecurityOperations: creds}

	tv := forkTV("")
	tv.SecOps = buildSecurityOperationsValues()
	require.NotNil(t, tv.SecOps)
	return tv
}

// renderDocs renders a template and decodes every YAML document in it.
func renderDocs(t *testing.T, path string, values any) []map[string]any {
	t.Helper()

	rendered := templates.ParseAndExecuteTemplate(context.Background(), &KubeaidConfigFileTemplates, path, values)
	require.NotContains(t, string(rendered), "{{", "no template delimiters may reach the argocd-apps Helm chart")

	decoder := yaml.NewDecoder(bytes.NewReader(rendered))
	var docs []map[string]any
	for {
		var doc map[string]any
		err := decoder.Decode(&doc)
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err, "rendered output must be valid YAML:\n%s", rendered)
		docs = append(docs, doc)
	}
	return docs
}

func dig(t *testing.T, m map[string]any, keys ...string) any {
	t.Helper()

	var cur any = m
	for _, key := range keys {
		asMap, ok := cur.(map[string]any)
		require.True(t, ok, "expected a map at %q in %v", key, keys)
		cur, ok = asMap[key]
		require.True(t, ok, "missing key %q (path %v)", key, keys)
	}
	return cur
}

func asMap(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	require.True(t, ok, "expected a map, got %T", v)
	return m
}

func asList(t *testing.T, v any) []any {
	t.Helper()
	l, ok := v.([]any)
	require.True(t, ok, "expected a list, got %T", v)
	return l
}

func asString(t *testing.T, v any) string {
	t.Helper()
	s, ok := v.(string)
	require.True(t, ok, "expected a string, got %T", v)
	return s
}

func digMap(t *testing.T, m map[string]any, keys ...string) map[string]any {
	t.Helper()
	return asMap(t, dig(t, m, keys...))
}

func digList(t *testing.T, m map[string]any, keys ...string) []any {
	t.Helper()
	return asList(t, dig(t, m, keys...))
}

func digString(t *testing.T, m map[string]any, keys ...string) string {
	t.Helper()
	return asString(t, dig(t, m, keys...))
}

func siemApps(t *testing.T, tv *TemplateValues) map[string]map[string]any {
	t.Helper()

	apps := map[string]map[string]any{}
	for _, doc := range renderDocs(t, siemAppsTmpl, tv) {
		assert.Equal(t, "Application", doc["kind"])
		name := digString(t, doc, "metadata", "name")
		apps[name] = doc
	}
	return apps
}

func TestSIEMApplications(t *testing.T) {
	tv := withSIEMConfig(t, siemTenant(1, "Tenant A"), siemTenant(2, `Tenant "B" & <Co>`))
	apps := siemApps(t, tv)
	require.Len(t, apps, 3)

	central := apps["security-operations"]
	require.NotNil(t, central)
	assert.Equal(t, "60", dig(t, central, "metadata", "labels", "kubeaid.io/sync-order"))
	assert.Equal(t, "security-operations", dig(t, central, "spec", "destination", "namespace"))
	centralSource := asMap(t, digList(t, central, "spec", "sources")[0])
	assert.Equal(t, "argocd-helm-charts/security-operations", centralSource["path"])
	assert.Equal(t, "master", centralSource["targetRevision"])
	assert.Equal(t,
		[]any{"$values/k8s/demo/argocd-apps/values-security-operations.yaml"},
		dig(t, centralSource, "helm", "valueFiles"))
	assert.Contains(t, dig(t, central, "spec", "syncPolicy", "syncOptions"), "CreateNamespace=true")

	for i, code := range []string{"001", "002"} {
		app := apps["wazuh-"+code]
		require.NotNil(t, app, "one Application per tenant")

		assert.Equal(t, "61", dig(t, app, "metadata", "labels", "kubeaid.io/sync-order"))
		assert.Equal(t, "master", dig(t, app, "metadata", "labels", "kubeaid.io/version"))
		assert.Equal(t, "wazuh-"+code, dig(t, app, "spec", "destination", "namespace"))
		assert.Contains(t, dig(t, app, "spec", "syncPolicy", "syncOptions"), "CreateNamespace=true")

		sources := digList(t, app, "spec", "sources")
		require.Len(t, sources, 2)
		source := asMap(t, sources[0])
		assert.Equal(t, "argocd-helm-charts/wazuh", source["path"])
		assert.Equal(t,
			[]any{"$values/k8s/demo/argocd-apps/values-wazuh-tenant.yaml"},
			dig(t, source, "helm", "valueFiles"))

		values := digMap(t, source, "helm", "valuesObject")
		assert.Equal(t, map[string]any{
			"enabled":          true,
			"externalIPs":      []any{siemAgentAddress},
			"registrationPort": 20000 + (i+1)*10 + 5,
			"eventsPort":       20000 + (i+1)*10 + 4,
		}, values["agentService"])

		assert.Equal(t, "tenant-"+code, dig(t, values, "wazuh", "certificates", "subject", "organization"))
		assert.Equal(t, 1, dig(t, values, "wazuh", "indexer", "replicas"))
		assert.Equal(t, "$2a$12$indexer."+code, dig(t, values, "wazuh", "indexer", "cred", "passwordHash"))
		assert.Equal(t, "$2a$12$dashboard."+code, dig(t, values, "wazuh", "dashboard", "cred", "passwordHash"))
		assert.Equal(t, "key-"+code, dig(t, values, "wazuh", "wazuh", "key"))

		host := "soc-wazuh-" + code + ".example.com"
		assert.Equal(t, host, dig(t, values, "wazuh", "dashboard", "ingress", "host"))
		tls := digList(t, values, "wazuh", "dashboard", "ingress", "tls")
		assert.Equal(t, []any{host}, dig(t, asMap(t, tls[0]), "hosts"))

		mappings := digMap(t, values, "wazuh", "dashboard", "sso", "oidc", "roleMappings")
		assert.Equal(t, []any{"administrator"}, dig(t, mappings, "allAccess", "backendRoles"))
		assert.Equal(t, []any{"analyst", "tenant-" + code}, dig(t, mappings, "readall", "backendRoles"))
		assert.Equal(t, []any{"analyst", "tenant-" + code}, dig(t, mappings, "kibanaUser", "backendRoles"))
	}

	t.Run("IRIS customer is the tenant name, escaped", func(t *testing.T) {
		extraConf := func(code string) string {
			source := asMap(t, digList(t, apps["wazuh-"+code], "spec", "sources")[0])
			return digString(t, source, "helm", "valuesObject", "wazuh", "wazuh", "master", "extraConf")
		}
		assert.Contains(t, extraConf("001"),
			`<options>{"min_level":10,"customer_name":"Tenant A"}</options>`)
		assert.Contains(t, extraConf("002"),
			"<options>{\"min_level\":10,\"customer_name\":\"Tenant \\\"B\\\" \\u0026 \\u003cCo\\u003e\"}</options>",
			"no raw XML markup characters in ossec.conf")
	})

	t.Run("managers register the MISP lists and drop the stock IoC pack", func(t *testing.T) {
		source := asMap(t, digList(t, apps["wazuh-001"], "spec", "sources")[0])
		conf := digString(t, source, "helm", "valuesObject", "wazuh", "wazuh", "master", "extraConf")
		for _, l := range []string{"misp-malware-hashes", "misp-malicious-ip", "misp-malicious-domains"} {
			assert.Contains(t, conf, "<list>etc/lists/"+l+"</list>")
		}
		assert.Contains(t, conf, "<rule_exclude>0999-malicious-ioc-rules.xml</rule_exclude>")
		// A rule_exclude rebuilds the ruleset from this block alone.
		assert.Contains(t, conf, "<rule_dir>etc/rules</rule_dir>")
		assert.Contains(t, conf, "<decoder_dir>etc/decoders</decoder_dir>")
	})
}

func TestSIEMChartRevision(t *testing.T) {
	tenant := siemTenant(1, "Tenant A")
	withSIEMConfig(t, tenant)
	config.ParsedGeneralConfig.Cluster.SecurityOperations.ChartRevision = "feat/security-operations"

	tv := forkTV("")
	tv.SecOps = buildSecurityOperationsValues()
	for name, app := range siemApps(t, tv) {
		source := asMap(t, digList(t, app, "spec", "sources")[0])
		assert.Equal(t, "feat/security-operations", source["targetRevision"], name)
		assert.Equal(t, "master", dig(t, app, "metadata", "labels", "kubeaid.io/version"), name)
		values := asMap(t, digList(t, app, "spec", "sources")[1])
		assert.Equal(t, "HEAD", values["targetRevision"], name)
	}

	// A feature branch of kubeaid-config for the values files.
	config.ParsedGeneralConfig.Cluster.SecurityOperations.ConfigRevision = "feature-branch"
	tv.SecOps = buildSecurityOperationsValues()
	for name, app := range siemApps(t, tv) {
		values := asMap(t, digList(t, app, "spec", "sources")[1])
		assert.Equal(t, "feature-branch", values["targetRevision"], name)
		assert.Equal(t, "values", values["ref"], name)
	}
}

func TestSIEMCentralValues(t *testing.T) {
	tv := withSIEMConfig(t, siemTenant(1, "Tenant A"), siemTenant(2, "Tenant B"))
	docs := renderDocs(t, siemValuesTmpl, tv)
	require.Len(t, docs, 1)
	values := docs[0]

	assert.Equal(t, "example.com", values["domain"])
	assert.Equal(t, "soc-wazuh.example.com", dig(t, values, "hosts", "wazuh"))
	assert.Equal(t, "soc-iris.example.com", dig(t, values, "hosts", "iris"))
	assert.Equal(t, "https://keycloak.example.com/auth", dig(t, values, "keycloak", "url"),
		"trailing slash trimmed")
	assert.Equal(t, "soc", dig(t, values, "keycloak", "realm"))
	assert.Equal(t, "agents.example.com", dig(t, values, "tenantWazuh", "agentHost"))
	assert.Equal(t, "soc-wazuh-", dig(t, values, "tenantWazuh", "hostPrefix"))

	assert.Equal(t, []any{
		map[string]any{
			"code": "001", "name": "Tenant A",
			"agentPorts": map[string]any{"registration": 20015, "events": 20014},
		},
		map[string]any{
			"code": "002", "name": "Tenant B",
			"agentPorts": map[string]any{"registration": 20025, "events": 20024},
		},
	}, values["tenants"])

	assert.Equal(t, map[string]any{"enabled": false, "dryRun": true}, values["reconciler"])

	assert.Equal(t, []any{
		map[string]any{
			"name": "001", "url": "https://wazuh.wazuh-001.svc:55000",
			"credentialsSecret": "wazuh-api-cred-001", "verifyTls": false,
		},
		map[string]any{
			"name": "002", "url": "https://wazuh.wazuh-002.svc:55000",
			"credentialsSecret": "wazuh-api-cred-002", "verifyTls": false,
		},
	}, dig(t, values, "misp", "wazuhCdbExport", "targets"))
	assert.Equal(t, true, dig(t, values, "misp", "wazuhCdbExport", "enabled"),
		"the tenant managers register the lists, so the export runs")

	central := digMap(t, values, "wazuh", "wazuh")
	assert.Equal(t, "wazuh-indexer-cred", dig(t, central, "indexer", "cred", "existingSecret"))
	assert.Equal(t, "$2a$12$central.indexer", dig(t, central, "indexer", "cred", "passwordHash"))
	assert.Equal(t, "wazuh-dashboard-cred", dig(t, central, "dashboard", "cred", "existingSecret"))
	assert.Equal(t, "$2a$12$central.dashboard", dig(t, central, "dashboard", "cred", "passwordHash"))
	assert.Equal(t, "soc-wazuh.example.com", dig(t, central, "dashboard", "ingress", "host"))
	assert.Equal(t, constants.ClusterIssuerLetsEncrypt,
		dig(t, central, "dashboard", "ingress", "annotations", "cert-manager.io/cluster-issuer"))
	assert.Equal(t, "wazuh-dashboard-oidc", dig(t, central, "dashboard", "sso", "oidc", "existingSecret"))
	assert.Equal(t, siemIssuer, dig(t, central, "dashboard", "sso", "oidc", "issuer"))
	assert.Equal(t, []any{"administrator"},
		dig(t, central, "dashboard", "sso", "oidc", "roleMappings", "allAccess", "backendRoles"))
	assert.Equal(t, []any{"analyst"},
		dig(t, central, "dashboard", "sso", "oidc", "roleMappings", "readall", "backendRoles"))

	// Overriding a list replaces the chart default, so the defaults are kept.
	egresses := digList(t, central, "indexer", "networkPolicy", "extraEgresses")
	assert.Equal(t, 9300, asMap(t, digList(t, asMap(t, egresses[0]), "ports")[0])["port"])

	assert.Equal(t, "soc-iris.example.com", dig(t, values, "dfir-iris", "ingress", "host"))
	assert.Equal(t, siemIssuer, dig(t, values, "dfir-iris", "authentication", "oidc", "issuerUrl"))
	assert.Equal(t, "soc-misp.example.com", dig(t, values, "misp", "misp", "instanceEnv", "ingressHostName"))
	assert.Equal(t, true, dig(t, values, "misp", "misp", "misp", "ingress", "enabled"))
	assert.Equal(t, "https://soc-velociraptor.example.com/app/index.html",
		dig(t, values, "velociraptor", "velociraptor", "gui", "publicUrl"))
	assert.Equal(t, "velociraptor-oidc",
		dig(t, values, "velociraptor", "velociraptor", "gui", "oidc", "existingSecret"))
}

func TestSIEMCentralValuesReconciler(t *testing.T) {
	withSIEMConfig(t)
	soc := config.ParsedGeneralConfig.Cluster.SecurityOperations
	dryRun := false
	soc.Reconciler = config.SecurityOperationsReconcilerConfig{Enabled: true, ImageTag: "v1.2.3", DryRun: &dryRun}
	tv := forkTV("")
	tv.SecOps = buildSecurityOperationsValues()

	values := renderDocs(t, siemValuesTmpl, tv)[0]
	assert.Equal(t, map[string]any{
		"enabled": true, "dryRun": false, "image": map[string]any{"tag": "v1.2.3"},
	}, values["reconciler"])
	assert.Equal(t, []any{}, values["tenants"])
	assert.Equal(t, []any{}, dig(t, values, "misp", "wazuhCdbExport", "targets"))
	// The Velociraptor API client publisher runs the reconciler image; the
	// chart fails the render when the two differ.
	assert.Equal(t, map[string]any{
		"enabled": true,
		"publisherImage": map[string]any{
			"repository": "ghcr.io/obmondo/siem-reconciler", "tag": "v1.2.3",
		},
	}, dig(t, values, "velociraptor", "velociraptor", "apiClient"))

	// A private registry: repository and tag together.
	soc.Reconciler.ImageRepository = "registry.example.com/soc/siem-reconciler"
	tv.SecOps = buildSecurityOperationsValues()
	values = renderDocs(t, siemValuesTmpl, tv)[0]
	assert.Equal(t, map[string]any{
		"repository": "registry.example.com/soc/siem-reconciler", "tag": "v1.2.3",
	}, dig(t, values, "reconciler", "image"))
	assert.NotContains(t, digMap(t, values, "reconciler"), "imagePullSecrets")
	assert.Equal(t, dig(t, values, "reconciler", "image"),
		dig(t, values, "velociraptor", "velociraptor", "apiClient", "publisherImage"))
	assert.NotContains(t, digMap(t, values, "velociraptor", "velociraptor", "apiClient"), "imagePullSecrets")

	soc.Reconciler.ImagePullSecrets = []string{"registry-pull"}
	tv.SecOps = buildSecurityOperationsValues()
	values = renderDocs(t, siemValuesTmpl, tv)[0]
	assert.Equal(t, []any{map[string]any{"name": "registry-pull"}},
		dig(t, values, "reconciler", "imagePullSecrets"))
	assert.Equal(t, []any{map[string]any{"name": "registry-pull"}},
		dig(t, values, "velociraptor", "velociraptor", "apiClient", "imagePullSecrets"))

	// Without the reconciler the chart default (no API client) stays.
	soc.Reconciler.Enabled = false
	tv.SecOps = buildSecurityOperationsValues()
	values = renderDocs(t, siemValuesTmpl, tv)[0]
	assert.NotContains(t, digMap(t, values, "velociraptor", "velociraptor"), "apiClient")
}

func TestSIEMTenantBaseValues(t *testing.T) {
	tv := withSIEMConfig(t, siemTenant(1, "Tenant A"))
	values := renderDocs(t, siemTenantValuesTmpl, tv)[0]

	assert.Equal(t, true, dig(t, values, "irisIntegration", "enabled"))
	assert.NotContains(t, values, "agentTcpRoutes", "agents come in through agentService")

	w := digMap(t, values, "wazuh")
	assert.Equal(t, "wazuh", w["fullnameOverride"])
	assert.Equal(t, false, dig(t, w, "agent", "enabled"))
	assert.Equal(t, "soc-ca", dig(t, w, "certificates", "issuer", "name"))
	assert.Equal(t, "ClusterIssuer", dig(t, w, "certificates", "issuer", "type"))
	assert.Equal(t, []any{"CN=wazuh-indexer,O=central,L=California,C=US"},
		dig(t, w, "indexer", "config", "extraNodesDn"))
	assert.Equal(t, true, dig(t, w, "autoreload", "enabled"))
	assert.Equal(t, "wazuh-indexer-cred", dig(t, w, "indexer", "cred", "existingSecret"))
	assert.Equal(t, "wazuh-dashboard-cred", dig(t, w, "dashboard", "cred", "existingSecret"))
	assert.Equal(t, "wazuh-api-cred", dig(t, w, "wazuh", "apiCred", "existingSecret"))
	assert.Equal(t, "wazuh-authd-pass", dig(t, w, "wazuh", "authd", "existingSecret"))
	assert.Equal(t, false, dig(t, w, "wazuh", "worker", "enabled"))
	rules := digString(t, w, "wazuh", "localRules")
	assert.Equal(t, 20, strings.Count(rules, "<rule id="), "IoC rules 99901-99920")
	assert.Contains(t, rules, `<rule id="99901" level="14">`)
	assert.NotContains(t, rules, "etc/lists/malicious-ioc/", "every rule reads the MISP lists")
	assert.Equal(t, "wazuh-dashboard-oidc", dig(t, w, "dashboard", "sso", "oidc", "existingSecret"))
	assert.Equal(t, siemIssuer, dig(t, w, "dashboard", "sso", "oidc", "issuer"))

	ports := digList(t, w, "wazuh", "master", "service", "ports")
	var names []string
	for _, p := range ports {
		names = append(names, asString(t, asMap(t, p)["name"]))
	}
	assert.Equal(t, []string{"registration", "api", "agents-events"}, names)

	ingresses := digList(t, w, "wazuh", "master", "networkPolicy", "extraIngresses")
	agents := asMap(t, ingresses[0])
	assert.Equal(t, "0.0.0.0/0", dig(t, asMap(t, asList(t, agents["from"])[0]), "ipBlock", "cidr"))
	assert.Len(t, agents["ports"], 2)

	indexerIngress := asMap(t, digList(t, w, "indexer", "networkPolicy", "extraIngresses")[0])
	assert.Equal(t, 9300, asMap(t, asList(t, indexerIngress["ports"])[0])["port"])
}

// TestSIEMThirdTenantAddsOnlyItsOwn: adding tenant 003 leaves every rendered
// line of 001 and 002 in place and adds lines that belong to 003 only.
func TestSIEMThirdTenantAddsOnlyItsOwn(t *testing.T) {
	render := func(tenants ...config.SecurityOperationsTenant) map[string]string {
		tv := withSIEMConfig(t, tenants...)
		out := map[string]string{}
		for _, tmpl := range []string{siemAppsTmpl, siemValuesTmpl, siemTenantValuesTmpl} {
			out[tmpl] = string(templates.ParseAndExecuteTemplate(
				context.Background(), &KubeaidConfigFileTemplates, tmpl, tv))
		}
		return out
	}

	two := render(siemTenant(1, "Tenant A"), siemTenant(2, "Tenant B"))
	three := render(siemTenant(1, "Tenant A"), siemTenant(2, "Tenant B"), siemTenant(3, "Tenant C"))

	assert.Equal(t, two[siemTenantValuesTmpl], three[siemTenantValuesTmpl],
		"the shared tenant values do not depend on the tenant list")

	for _, tmpl := range []string{siemAppsTmpl, siemValuesTmpl} {
		added := addedLines(two[tmpl], three[tmpl])
		require.NotEmpty(t, added, tmpl)

		// Every added line is 003's own or generic structure of its entry.
		joined := strings.Join(added, "\n")
		assert.Contains(t, joined, "003", tmpl)
		assert.NotContains(t, joined, "001", tmpl)
		assert.NotContains(t, joined, "002", tmpl)
		assert.Empty(t, addedLines(three[tmpl], two[tmpl]), "%s: nothing of 001/002 changes", tmpl)
	}

	apps := siemApps(t, withSIEMConfig(t, siemTenant(1, "Tenant A"), siemTenant(2, "Tenant B"), siemTenant(3, "Tenant C")))
	assert.Len(t, apps, 4)
	assert.Contains(t, apps, "wazuh-003")
}

// addedLines returns the lines of b not in a (as a multiset).
func addedLines(a, b string) []string {
	count := map[string]int{}
	for _, line := range strings.Split(a, "\n") {
		count[line]++
	}
	var added []string
	for _, line := range strings.Split(b, "\n") {
		if count[line] > 0 {
			count[line]--
			continue
		}
		added = append(added, line)
	}
	return added
}

func TestSIEMSecretFiles(t *testing.T) {
	withSIEMConfig(t, siemTenant(1, "Tenant A"), siemTenant(2, "Tenant B"))

	files := securityOperationsSecretFiles()
	var paths []string
	for _, f := range files {
		paths = append(paths, f.RelativePath)
	}
	assert.Equal(t, []string{
		"sealed-secrets/security-operations/wazuh-indexer-cred.yaml",
		"sealed-secrets/security-operations/wazuh-dashboard-cred.yaml",
		"sealed-secrets/wazuh-001/wazuh-indexer-cred.yaml",
		"sealed-secrets/wazuh-001/wazuh-dashboard-cred.yaml",
		"sealed-secrets/wazuh-001/wazuh-api-cred.yaml",
		"sealed-secrets/wazuh-001/wazuh-authd-pass.yaml",
		"sealed-secrets/wazuh-002/wazuh-indexer-cred.yaml",
		"sealed-secrets/wazuh-002/wazuh-dashboard-cred.yaml",
		"sealed-secrets/wazuh-002/wazuh-api-cred.yaml",
		"sealed-secrets/wazuh-002/wazuh-authd-pass.yaml",
	}, paths)

	wantKeys := map[string]map[string]any{
		"wazuh-indexer-cred":   {"INDEXER_USERNAME": "admin", "INDEXER_PASSWORD": "indexer-002"},
		"wazuh-dashboard-cred": {"DASHBOARD_USERNAME": "kibanaserver", "DASHBOARD_PASSWORD": "dashboard-002"},
		"wazuh-api-cred":       {"API_USERNAME": "wazuh-wui", "API_PASSWORD": "Aa1.api002"},
		"wazuh-authd-pass":     {"authd.pass": "authd-002"},
	}
	for _, f := range files[6:] {
		docs := renderDocs(t, "templates/"+f.TemplateName, f.Data)
		require.Len(t, docs, 1)
		secret := docs[0]
		assert.Equal(t, "Secret", secret["kind"])
		assert.Equal(t, "wazuh-002", dig(t, secret, "metadata", "namespace"))
		name := digString(t, secret, "metadata", "name")
		assert.Equal(t, wantKeys[name], secret["stringData"], name)
	}

	central := renderDocs(t, "templates/"+files[0].TemplateName, files[0].Data)[0]
	assert.Equal(t, "security-operations", dig(t, central, "metadata", "namespace"))
	assert.Equal(t, "central-indexer", dig(t, central, "stringData", "INDEXER_PASSWORD"))
}

func TestSIEMTemplateNames(t *testing.T) {
	withSIEMConfig(t, siemTenant(1, "Tenant A"))
	names := getEmbeddedNonSecretTemplateNames()
	for _, name := range constants.SecurityOperationsNonSecretTemplateNames {
		assert.Contains(t, names, name)
	}

	config.ParsedGeneralConfig.Cluster.SecurityOperations.Enabled = false
	names = getEmbeddedNonSecretTemplateNames()
	for _, name := range constants.SecurityOperationsNonSecretTemplateNames {
		assert.NotContains(t, names, name)
	}
	assert.Nil(t, buildSecurityOperationsValues())
	assert.Empty(t, securityOperationsSecretFiles())
}

// writeTestSealingCert writes a self-signed RSA certificate for sealing.
func writeTestSealingCert(t *testing.T) string {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "sealed-secrets-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)

	certPath := filepath.Join(t.TempDir(), "cert.pem")
	require.NoError(t, os.WriteFile(certPath,
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600))
	return certPath
}

// TestRenderSecurityOperations renders into an empty cluster directory with a
// local sealing certificate: only the SIEM files appear, the Secrets are
// sealed (no plaintext password on disk), and a second run leaves the sealed
// files byte-identical.
func TestRenderSecurityOperations(t *testing.T) {
	withSIEMConfig(t, siemTenant(1, "Tenant A"), siemTenant(2, "Tenant B"))

	origCert := kubernetes.SealingCertSource
	kubernetes.SealingCertSource = writeTestSealingCert(t)
	t.Cleanup(func() { kubernetes.SealingCertSource = origCert })

	dir := t.TempDir()
	written, err := RenderSecurityOperations(context.Background(), dir)
	require.NoError(t, err)
	require.Len(t, written, 3+2+2*4)

	var onDisk []string
	require.NoError(t, filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(dir, p)
			onDisk = append(onDisk, rel)
		}
		return err
	}))
	assert.ElementsMatch(t, written, onDisk, "nothing but the SIEM files is written")

	sealedPath := filepath.Join(dir, "sealed-secrets/wazuh-002/wazuh-api-cred.yaml")
	sealed, err := os.ReadFile(sealedPath)
	require.NoError(t, err)
	assert.Contains(t, string(sealed), "kind: SealedSecret")
	assert.NotContains(t, string(sealed), "Aa1.api002")

	_, err = RenderSecurityOperations(context.Background(), dir)
	require.NoError(t, err)
	again, err := os.ReadFile(sealedPath)
	require.NoError(t, err)
	assert.Equal(t, string(sealed), string(again), "unchanged plaintext is not re-encrypted")
}

// TestSIEMKeycloakHostAlias pins the Keycloak host in every SOC pod.
func TestSIEMKeycloakHostAlias(t *testing.T) {
	withSIEMConfig(t, siemTenant(1, "Tenant A"))
	config.ParsedGeneralConfig.Cluster.SecurityOperations.Keycloak.HostAliasIP = "10.0.0.10"
	tv := forkTV("")
	tv.SecOps = buildSecurityOperationsValues()

	want := []any{map[string]any{"ip": "10.0.0.10", "hostnames": []any{"keycloak.example.com"}}}
	central := renderDocs(t, siemValuesTmpl, tv)[0]
	for _, keys := range [][]string{
		{"reconciler", "hostAliases"},
		{"wazuh", "wazuh", "indexer", "extraSpec", "pod", "hostAliases"},
		{"wazuh", "wazuh", "dashboard", "extraSpec", "pod", "hostAliases"},
		{"velociraptor", "velociraptor", "hostAliases"},
		{"dfir-iris", "hostAliases"},
		{"misp", "misp", "hostAliases"},
	} {
		assert.Equal(t, want, dig(t, central, keys...), "%v", keys)
	}

	tenant := renderDocs(t, siemTenantValuesTmpl, tv)[0]
	for _, keys := range [][]string{
		{"wazuh", "indexer", "extraSpec", "pod", "hostAliases"},
		{"wazuh", "dashboard", "extraSpec", "pod", "hostAliases"},
	} {
		assert.Equal(t, want, dig(t, tenant, keys...), "%v", keys)
	}

	// Without it nothing is pinned.
	config.ParsedGeneralConfig.Cluster.SecurityOperations.Keycloak.HostAliasIP = ""
	tv.SecOps = buildSecurityOperationsValues()
	central = renderDocs(t, siemValuesTmpl, tv)[0]
	assert.NotContains(t, digMap(t, central, "reconciler"), "hostAliases")
	assert.NotContains(t, digMap(t, central, "dfir-iris"), "hostAliases")
}

// TestSIEMOptionalComponents covers the Velociraptor client route, the MISP
// Valkey password and IRIS shared storage: rendered only when set.
func TestSIEMOptionalComponents(t *testing.T) {
	securityOperationsMISPRedisPassword = ""
	withSIEMConfig(t, siemTenant(1, "Tenant A"))
	tv := forkTV("")
	tv.SecOps = buildSecurityOperationsValues()

	central := renderDocs(t, siemValuesTmpl, tv)[0]
	assert.NotContains(t, digMap(t, central, "velociraptor"), "frontendTcpRoute")
	assert.NotContains(t, digMap(t, central, "dfir-iris"), "persistence")
	assert.NotContains(t, digMap(t, central, "misp", "misp", "env"), "redisPassword")

	soc := config.ParsedGeneralConfig.Cluster.SecurityOperations
	soc.VelociraptorEntryPoint = "velociraptor"
	soc.SharedStorageClass = "shared-fs"
	securityOperationsMISPRedisPassword = "redis-secret"
	t.Cleanup(func() { securityOperationsMISPRedisPassword = "" })
	tv.SecOps = buildSecurityOperationsValues()
	central = renderDocs(t, siemValuesTmpl, tv)[0]

	assert.Equal(t, map[string]any{"enabled": true, "entryPoint": "velociraptor", "host": "soc-velociraptor.example.com"},
		dig(t, central, "velociraptor", "frontendTcpRoute"))
	assert.Len(t, digList(t, central, "velociraptor", "frontendAllowedFrom"), 1)
	assert.Equal(t, map[string]any{"storageClass": "shared-fs", "accessModes": []any{"ReadWriteMany"}},
		dig(t, central, "dfir-iris", "persistence"))
	assert.Equal(t, "redis-secret", dig(t, central, "misp", "misp", "env", "redisPassword"))
	assert.Equal(t, "redis-secret", dig(t, central, "misp", "misp", "valkey", "auth", "aclUsers", "default", "password"))
}

// The MISP Valkey password survives renders and replaces the chart default.
func TestMISPRedisPasswordFromClusterDir(t *testing.T) {
	dir := t.TempDir()
	first, err := mispRedisPasswordFromClusterDir(dir)
	require.NoError(t, err)
	assert.Len(t, first, 32)

	write := func(pw string) {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "argocd-apps"), 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(dir, securityOperationsCentralValuesFile),
			[]byte("misp:\n  misp:\n    env:\n      redisPassword: \""+pw+"\"\n"), 0o600))
	}
	write("kept-password")
	kept, err := mispRedisPasswordFromClusterDir(dir)
	require.NoError(t, err)
	assert.Equal(t, "kept-password", kept)

	write(mispChartDefaultRedisPassword)
	fresh, err := mispRedisPasswordFromClusterDir(dir)
	require.NoError(t, err)
	assert.NotEqual(t, mispChartDefaultRedisPassword, fresh)
}

// The IRIS Keycloak sync is on, against the realm and the iris-sync client.
func TestSIEMIRISKeycloakSync(t *testing.T) {
	withSIEMConfig(t, siemTenant(1, "Tenant A"))
	tv := forkTV("")
	tv.SecOps = buildSecurityOperationsValues()
	central := renderDocs(t, siemValuesTmpl, tv)[0]
	assert.Equal(t, true, dig(t, central, "dfir-iris", "keycloakSync", "enabled"))
	assert.Equal(t, "iris-sync", dig(t, central, "dfir-iris", "keycloakSync", "keycloak", "clientId"))
	assert.Equal(t, "soc", dig(t, central, "dfir-iris", "keycloakSync", "keycloak", "realm"))
	assert.Equal(t, "iris-keycloak-sync", dig(t, central, "dfir-iris", "keycloakSync", "existingSecret"))
}

// AI triage is off and dry-run by default; downloadModel opens Ollama's egress
// and pulls the model.
const testAIModel = "qwen2.5:7b"

func TestSIEMAITriage(t *testing.T) {
	withSIEMConfig(t, siemTenant(1, "Tenant A"))
	tv := forkTV("")
	tv.SecOps = buildSecurityOperationsValues()
	central := renderDocs(t, siemValuesTmpl, tv)[0]
	assert.Equal(t, map[string]any{"enabled": false, "dryRun": true, "model": constants.SecurityOperationsDefaultAIModel},
		dig(t, central, "dfir-iris", "aiTriage"))
	assert.NotContains(t, central, "ollama")

	dryRun := false
	soc := config.ParsedGeneralConfig.Cluster.SecurityOperations
	soc.AITriage = config.SecurityOperationsAITriageConfig{Enabled: true, DryRun: &dryRun, Model: testAIModel, DownloadModel: true}
	tv.SecOps = buildSecurityOperationsValues()
	central = renderDocs(t, siemValuesTmpl, tv)[0]
	assert.Equal(t, map[string]any{"enabled": true, "dryRun": false, "model": testAIModel},
		dig(t, central, "dfir-iris", "aiTriage"))
	assert.Equal(t, []any{testAIModel}, dig(t, central, "ollama", "ollama", "ollama", "models", "pull"))
	assert.Equal(t, true, dig(t, central, "ollama", "networkPolicy", "allowModelDownload"))
}
