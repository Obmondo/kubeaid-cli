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

// stringSlice asserts v is a []any of strings (as produced by
// sigs.k8s.io/yaml unmarshalling into map[string]any) and returns it as
// a plain []string for comparison.
func stringSlice(t *testing.T, v any) []string {
	t.Helper()

	raw, ok := v.([]any)
	require.True(t, ok, "expected a slice, got %T", v)

	out := make([]string, len(raw))
	for i, e := range raw {
		s, ok := e.(string)
		require.True(t, ok, "expected element %d to be a string, got %T", i, e)
		out[i] = s
	}
	return out
}

// TestNetBirdOperatorValuesTemplate covers the four independently-gated
// blocks (managementURL, networkRouter, networkResources, clusterProxy)
// and the top-level HasNetBirdOperatorValues gate.
func TestNetBirdOperatorValuesTemplate(t *testing.T) {
	t.Run("all features on: everything nests under netbird-operator", func(t *testing.T) {
		tv := &TemplateValues{
			NetBirdManagementURL: "https://netbird.vpn.acme.com",
			NetBird: &config.NetBirdConfig{
				NetworkRouter: &config.NetBirdNetworkRouterConfig{
					Enabled:  true,
					DNSZone:  "vpn.acme.com",
					Replicas: 3,
				},
				NetworkResources: []config.NetBirdNetworkResourceConfig{
					{
						Name:      "internal-api",
						Namespace: "backend",
						Service:   "internal-api",
						Groups:    []string{"engineering", "sre"},
					},
				},
				ClusterProxy: &config.NetBirdClusterProxyConfig{
					Enabled:     true,
					ClusterName: "acme-prod",
					RBAC: []config.NetBirdClusterProxyRBACConfig{
						{Group: "engineering", ClusterRole: "edit"},
						{Group: "sre", ClusterRole: "cluster-admin"},
					},
				},
			},
			NetBirdNetworkRouterEnabled: true,
			NetBirdClusterProxyEnabled:  true,
			HasNetBirdOperatorValues:    true,
		}

		raw, parsed := renderNetBirdOperatorValues(t, tv)
		require.NotEmpty(t, strings.TrimSpace(raw))
		require.Len(t, parsed, 1, "only the netbird-operator top-level key should be rendered")

		op := subMap(t, parsed, "netbird-operator")

		assert.Equal(t, "https://netbird.vpn.acme.com", op["managementURL"])

		router := subMap(t, op, "networkRouter")
		assert.Equal(t, true, router["enabled"])
		assert.Equal(t, "vpn.acme.com", router["dnsZone"])
		assert.EqualValues(t, 3, router["replicas"])

		resourcesRaw, ok := op["networkResources"].([]any)
		require.True(t, ok, "networkResources must be a list")
		require.Len(t, resourcesRaw, 1)
		res, ok := resourcesRaw[0].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "internal-api", res["name"])
		assert.Equal(t, "backend", res["namespace"])
		assert.Equal(t, "internal-api", res["service"])
		assert.Equal(t, []string{"engineering", "sre"}, stringSlice(t, res["groups"]))

		proxy := subMap(t, op, "clusterProxy")
		assert.Equal(t, true, proxy["enabled"])
		assert.Equal(t, "acme-prod", proxy["clusterName"])

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

	// Regression: managementURL-only output must stay exactly
	// `netbird-operator: {managementURL: ...}`, unchanged by the new blocks.
	t.Run("managementURL only: regression, matches pre-change shape exactly", func(t *testing.T) {
		tv := &TemplateValues{
			NetBirdManagementURL: "https://netbird.vpn.acme.com",
			// NetBird nil, every *Enabled flag false — matches
			// getTemplateValues' output for a netbird block with no
			// operator features configured.
			HasNetBirdOperatorValues: true,
		}

		_, parsed := renderNetBirdOperatorValues(t, tv)

		require.Len(t, parsed, 1, "exactly one top-level key")
		op := subMap(t, parsed, "netbird-operator")
		require.Len(t, op, 1, "exactly one key under netbird-operator")
		assert.Equal(t, "https://netbird.vpn.acme.com", op["managementURL"])
	})

	t.Run("only networkRouter enabled: renders just that block", func(t *testing.T) {
		tv := &TemplateValues{
			NetBird: &config.NetBirdConfig{
				NetworkRouter: &config.NetBirdNetworkRouterConfig{
					Enabled:  true,
					DNSZone:  "vpn.acme.com",
					Replicas: 1,
				},
			},
			NetBirdNetworkRouterEnabled: true,
			HasNetBirdOperatorValues:    true,
		}

		_, parsed := renderNetBirdOperatorValues(t, tv)
		op := subMap(t, parsed, "netbird-operator")
		require.Len(t, op, 1, "only networkRouter should render")

		router := subMap(t, op, "networkRouter")
		assert.Equal(t, true, router["enabled"])
		assert.Equal(t, "vpn.acme.com", router["dnsZone"])
		assert.EqualValues(t, 1, router["replicas"])
	})

	t.Run("only networkResources set: renders just that block", func(t *testing.T) {
		tv := &TemplateValues{
			NetBird: &config.NetBirdConfig{
				NetworkResources: []config.NetBirdNetworkResourceConfig{
					{Name: "db", Namespace: "data", Service: "postgres", Groups: []string{"dba"}},
				},
			},
			HasNetBirdOperatorValues: true,
		}

		_, parsed := renderNetBirdOperatorValues(t, tv)
		op := subMap(t, parsed, "netbird-operator")
		require.Len(t, op, 1, "only networkResources should render")

		resourcesRaw, ok := op["networkResources"].([]any)
		require.True(t, ok)
		require.Len(t, resourcesRaw, 1)

		res, ok := resourcesRaw[0].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "db", res["name"])
		assert.Equal(t, "data", res["namespace"])
		assert.Equal(t, "postgres", res["service"])
		assert.Equal(t, []string{"dba"}, stringSlice(t, res["groups"]))
	})

	t.Run("only clusterProxy enabled: renders just that block", func(t *testing.T) {
		tv := &TemplateValues{
			NetBird: &config.NetBirdConfig{
				ClusterProxy: &config.NetBirdClusterProxyConfig{
					Enabled:     true,
					ClusterName: "acme-prod",
				},
			},
			NetBirdClusterProxyEnabled: true,
			HasNetBirdOperatorValues:   true,
		}

		_, parsed := renderNetBirdOperatorValues(t, tv)
		op := subMap(t, parsed, "netbird-operator")
		require.Len(t, op, 1, "only clusterProxy should render")

		proxy := subMap(t, op, "clusterProxy")
		assert.Equal(t, true, proxy["enabled"])
		assert.Equal(t, "acme-prod", proxy["clusterName"])
		_, hasRBAC := proxy["rbac"]
		assert.False(t, hasRBAC, "rbac key must be absent when no RBAC entries are configured")
	})

	t.Run("HasNetBirdOperatorValues false: renders no netbird-operator key at all", func(t *testing.T) {
		tv := &TemplateValues{}

		raw, parsed := renderNetBirdOperatorValues(t, tv)
		assert.Empty(t, parsed, "no top-level keys expected")
		assert.Empty(t, strings.TrimSpace(raw), "rendered output should be effectively empty/whitespace")
	})
}
