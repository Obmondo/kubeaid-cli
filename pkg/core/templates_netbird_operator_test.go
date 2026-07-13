// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package core

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/templates"
)

// renderNetBirdOperatorValues renders values-netbird-operator.yaml.tmpl via
// the production template pipeline and parses the result into a generic
// map for structural assertions.
func renderNetBirdOperatorValues(t *testing.T, tv *TemplateValues) (string, map[string]any) {
	t.Helper()

	const tmplPath = "templates/argocd-apps/values-netbird-operator.yaml.tmpl"
	rendered := templates.ParseAndExecuteTemplate(
		context.Background(), &KubeaidConfigFileTemplates, tmplPath, tv,
	)

	var parsed map[string]any
	require.NoError(t, yaml.Unmarshal(rendered, &parsed),
		"rendered output must be valid YAML:\n%s", rendered)

	return string(rendered), parsed
}

// subMap asserts m[key] is present and is itself a map, and returns it.
func subMap(t *testing.T, m map[string]any, key string) map[string]any {
	t.Helper()

	v, ok := m[key]
	require.True(t, ok, "expected key %q in %#v", key, m)

	sub, ok := v.(map[string]any)
	require.True(t, ok, "expected %q to be a map, got %T", key, v)

	return sub
}

// TestNetBirdOperatorValuesTemplate covers values-netbird-operator.yaml.tmpl:
// the whole overlay is gated on NetBirdOperatorEnabled, but
// clusterName / groups / networkRouter / networkResources / clusterProxy render
// as TOP-LEVEL keys (siblings of netbird-operator:, not nested under it) — only
// managementURL nests under netbird-operator:, and only when it's non-empty.
//
// clusterName is emitted whenever the router or proxy is enabled; the chart
// derives everything from it (the router + proxy names and the two Group CRs
// k8s-<name> and k8s-<name>-access). groups is a flat list of EXTRA groups this
// cluster owns (NetBird.Groups), gated independently — a cluster can own groups
// while exposing neither a router nor a proxy.
//
// Fixtures mirror getTemplateValues' invariants: NetBirdRouterEnabled=true
// implies NetBird != nil (dnsZone comes from .NetBird.DNSZone), and
// NetBirdClusterProxyEnabled=true implies NetBird.ClusterProxy != nil —
// violating either would nil-deref inside the template.
func TestNetBirdOperatorValuesTemplate(t *testing.T) {
	t.Run("operator+router+clusterProxy all on: clusterName/router/resources/clusterProxy are top-level, managementURL nests under netbird-operator", func(t *testing.T) {
		tv := &TemplateValues{
			ClusterConfig:          config.ClusterConfig{Name: "acme-prod"},
			NetBirdOperatorEnabled: true,
			NetBirdManagementURL:   "https://netbird.vpn.acme.com",
			NetBird: &config.NetBirdConfig{
				DNSZone: "mesh.acme.com",
				ClusterProxy: &config.NetBirdClusterProxyConfig{
					Enabled: true,
					RBAC: []config.NetBirdClusterProxyRBACConfig{
						{Group: "engineering", ClusterRole: "edit"},
						{Group: "sre", ClusterRole: "cluster-admin"},
					},
				},
			},
			NetBirdRouterEnabled:       true,
			NetBirdClusterProxyEnabled: true,
		}

		_, parsed := renderNetBirdOperatorValues(t, tv)

		// clusterName, networkRouter, networkResources, and clusterProxy are
		// top-level siblings of netbird-operator — NOT nested under it.
		require.Len(t, parsed, 5)
		for _, key := range []string{"netbird-operator", "clusterName", "networkRouter", "networkResources", "clusterProxy"} {
			assert.Contains(t, parsed, key)
		}

		// The chart derives the groups (k8s-<name>, k8s-<name>-access) and the
		// router/proxy names from clusterName — the CLI emits no group names.
		assert.Equal(t, "acme-prod", parsed["clusterName"])
		assert.NotContains(t, parsed, "groups", "no cluster.netbird.groups configured — groups must be absent")

		op := subMap(t, parsed, "netbird-operator")
		require.Len(t, op, 1, "netbird-operator carries only managementURL")
		assert.Equal(t, "https://netbird.vpn.acme.com", op["managementURL"])

		router := subMap(t, parsed, "networkRouter")
		assert.Equal(t, true, router["enabled"])
		assert.NotContains(t, router, "name", "the chart names the router after clusterName")
		assert.Equal(t, "mesh.acme.com", router["dnsZone"])
		assert.EqualValues(t, 1, router["replicas"])

		resourcesRaw, ok := parsed["networkResources"].([]any)
		require.True(t, ok, "networkResources must be a list")
		require.Len(t, resourcesRaw, 1, "exactly the one hardcoded traefik-internal resource")
		res, ok := resourcesRaw[0].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "traefik-internal", res["name"])
		assert.Equal(t, "traefik", res["namespace"])
		assert.Equal(t, "traefik-internal", res["service"])
		_, hasGroups := res["groups"]
		assert.False(t, hasGroups, "traefik-internal carries no groups of its own — it joins the derived cluster group chart-side")

		proxy := subMap(t, parsed, "clusterProxy")
		assert.Equal(t, true, proxy["enabled"])
		assert.NotContains(t, proxy, "clusterName", "the chart derives the proxy name from the top-level clusterName")

		rbacRaw, ok := proxy["rbac"].([]any)
		require.True(t, ok, "rbac must be a list")
		require.Len(t, rbacRaw, 2)

		rbac0, ok := rbacRaw[0].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "engineering", rbac0["group"])
		assert.Equal(t, "edit", rbac0["clusterRole"])

		rbac1, ok := rbacRaw[1].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "sre", rbac1["group"])
		assert.Equal(t, "cluster-admin", rbac1["clusterRole"])
	})

	t.Run("managementURL only: router and clusterProxy disabled, no top-level leakage", func(t *testing.T) {
		tv := &TemplateValues{
			NetBirdOperatorEnabled: true,
			NetBirdManagementURL:   "https://netbird.vpn.acme.com",
			// NetBird nil, NetBirdRouterEnabled/NetBirdClusterProxyEnabled
			// false — matches getTemplateValues' output for a netbird block
			// with no dnsZone/clusterProxy configured.
		}

		_, parsed := renderNetBirdOperatorValues(t, tv)

		require.Len(t, parsed, 1, "only netbird-operator should render")
		op := subMap(t, parsed, "netbird-operator")
		require.Len(t, op, 1)
		assert.Equal(t, "https://netbird.vpn.acme.com", op["managementURL"])
	})

	t.Run("router enabled, no managementURL or clusterProxy: only clusterName + networkRouter/networkResources render", func(t *testing.T) {
		tv := &TemplateValues{
			ClusterConfig:          config.ClusterConfig{Name: "acme-prod"},
			NetBirdOperatorEnabled: true,
			NetBird: &config.NetBirdConfig{
				DNSZone: "mesh.acme.com",
			},
			NetBirdRouterEnabled: true,
		}

		_, parsed := renderNetBirdOperatorValues(t, tv)

		require.Len(t, parsed, 3)
		assert.NotContains(t, parsed, "netbird-operator", "empty managementURL means no netbird-operator: key at all")
		assert.NotContains(t, parsed, "clusterProxy")
		assert.Equal(t, "acme-prod", parsed["clusterName"])

		router := subMap(t, parsed, "networkRouter")
		assert.Equal(t, true, router["enabled"])
		assert.NotContains(t, router, "name", "the chart names the router after clusterName")
		assert.Equal(t, "mesh.acme.com", router["dnsZone"])
		assert.EqualValues(t, 1, router["replicas"])

		resourcesRaw, ok := parsed["networkResources"].([]any)
		require.True(t, ok)
		require.Len(t, resourcesRaw, 1)
		res, ok := resourcesRaw[0].(map[string]any)
		require.True(t, ok)
		_, hasGroups := res["groups"]
		assert.False(t, hasGroups, "traefik-internal joins the derived cluster group, not its own")
	})

	t.Run("clusterProxy enabled alone: only clusterName + clusterProxy render", func(t *testing.T) {
		tv := &TemplateValues{
			ClusterConfig:          config.ClusterConfig{Name: "acme-prod"},
			NetBirdOperatorEnabled: true,
			NetBird: &config.NetBirdConfig{
				ClusterProxy: &config.NetBirdClusterProxyConfig{
					Enabled: true,
				},
			},
			NetBirdClusterProxyEnabled: true,
		}

		_, parsed := renderNetBirdOperatorValues(t, tv)

		require.Len(t, parsed, 2)
		assert.NotContains(t, parsed, "netbird-operator")
		assert.NotContains(t, parsed, "networkRouter")
		assert.NotContains(t, parsed, "networkResources")
		assert.Equal(t, "acme-prod", parsed["clusterName"],
			"the chart derives the proxy name + cluster/access groups from it")

		proxy := subMap(t, parsed, "clusterProxy")
		assert.Equal(t, true, proxy["enabled"])
		assert.NotContains(t, proxy, "clusterName", "the chart derives the proxy name from the top-level clusterName")
		_, hasRBAC := proxy["rbac"]
		assert.False(t, hasRBAC, "rbac key must be absent when no RBAC entries are configured")
	})

	t.Run("extra groups only: flat groups list renders without clusterName, router or proxy", func(t *testing.T) {
		tv := &TemplateValues{
			ClusterConfig:          config.ClusterConfig{Name: "acme-prod"},
			NetBirdOperatorEnabled: true,
			NetBird: &config.NetBirdConfig{
				Groups: []string{"product", "operation-lvl1", "oncall"},
			},
			// Router and proxy both off: this cluster OWNS groups but exposes
			// nothing, so it claims no mesh identity and clusterName must not
			// be emitted.
		}

		_, parsed := renderNetBirdOperatorValues(t, tv)

		require.Len(t, parsed, 1, "only the groups list renders")
		assert.NotContains(t, parsed, "clusterName",
			"neither router nor proxy is on — this cluster claims no mesh identity")

		assert.Equal(t, []any{"product", "operation-lvl1", "oncall"}, parsed["groups"],
			"groups mirrors cluster.netbird.groups verbatim, in order")
	})

	t.Run("clusterProxy + extra groups: clusterName and the groups list both render", func(t *testing.T) {
		tv := &TemplateValues{
			ClusterConfig:          config.ClusterConfig{Name: "acme-prod"},
			NetBirdOperatorEnabled: true,
			NetBird: &config.NetBirdConfig{
				ClusterProxy: &config.NetBirdClusterProxyConfig{Enabled: true},
				Groups:       []string{"product"},
			},
			NetBirdClusterProxyEnabled: true,
		}

		_, parsed := renderNetBirdOperatorValues(t, tv)

		assert.Equal(t, "acme-prod", parsed["clusterName"])
		assert.Equal(t, []any{"product"}, parsed["groups"])
	})

	t.Run("NetBirdOperatorEnabled false: renders nothing, even with router/proxy/managementURL set", func(t *testing.T) {
		tv := &TemplateValues{
			NetBirdOperatorEnabled: false,
			NetBirdManagementURL:   "https://netbird.vpn.acme.com",
			NetBird: &config.NetBirdConfig{
				DNSZone: "mesh.acme.com",
				ClusterProxy: &config.NetBirdClusterProxyConfig{
					Enabled: true,
				},
			},
			NetBirdRouterEnabled:       true,
			NetBirdClusterProxyEnabled: true,
		}

		raw, parsed := renderNetBirdOperatorValues(t, tv)
		assert.Empty(t, parsed, "no top-level keys expected")
		assert.Empty(t, strings.TrimSpace(raw), "rendered output should be effectively empty/whitespace")
	})
}
