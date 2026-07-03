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

// renderTemplateToMap renders a template via the production pipeline and
// unmarshals the result into a generic map.
func renderTemplateToMap(t *testing.T, path string, tv *TemplateValues) map[string]any {
	t.Helper()

	rendered := templates.ParseAndExecuteTemplate(
		context.Background(), &KubeaidConfigFileTemplates, path, tv,
	)

	var parsed map[string]any
	require.NoError(t, yaml.Unmarshal(rendered, &parsed),
		"rendered output must be valid YAML:\n%s", rendered)
	return parsed
}

// forkTV returns a TemplateValues carrying the fork fields the argocd-app
// manifests reference, plus the given NetBird API key.
func forkTV(apiKey string) *TemplateValues {
	tv := &TemplateValues{NetBirdAPIKey: apiKey}
	tv.KubeaidFork = config.KubeAidForkConfig{URL: "https://example.test/KubeAid.git", Version: "master"}
	tv.KubeaidConfigFork = config.KubeaidConfigForkConfig{URL: "https://example.test/cfg.git", Directory: "demo"}
	return tv
}

// TestTraefikValuesTemplate_NoInternalBlock: the public values-traefik overlay
// renders only the base traefik: block. The internal Traefik moved to its own
// ArgoCD app (values-traefik-internal), so there's no traefik-internal key
// here — regardless of the NetBird API key.
func TestTraefikValuesTemplate_NoInternalBlock(t *testing.T) {
	for _, apiKey := range []string{"", "nb-mgmt-token"} {
		parsed := renderTemplateToMap(t,
			"templates/argocd-apps/values-traefik.yaml.tmpl", forkTV(apiKey))
		assert.Contains(t, parsed, "traefik")
		assert.NotContains(t, parsed, "traefik-internal")
	}
}

// TestTraefikInternalApp: the traefik-internal ArgoCD Application and its
// values overlay render only when secrets.yaml carries netbird.apiKey (there's
// no mesh to reach the ClusterIP-only instance over without a Mgmt key).
func TestTraefikInternalApp(t *testing.T) {
	const (
		appTmpl    = "templates/argocd-apps/templates/traefik-internal.yaml.tmpl"
		valuesTmpl = "templates/argocd-apps/values-traefik-internal.yaml.tmpl"
	)

	t.Run("apiKey set: Application + ClusterIP internal values render", func(t *testing.T) {
		tv := forkTV("nb-mgmt-token")

		app := renderTemplateToMap(t, appTmpl, tv)
		assert.Equal(t, "Application", app["kind"])
		assert.Equal(t, "traefik-internal", subMap(t, app, "metadata")["name"])

		values := renderTemplateToMap(t, valuesTmpl, tv)
		traefik := subMap(t, values, "traefik")
		assert.Equal(t, "traefik-internal", traefik["fullnameOverride"],
			"Service/IngressClass must be named traefik-internal so the NetworkResource resolves")

		// ClusterIP only — no public LB.
		assert.Equal(t, "ClusterIP", subMap(t, subMap(t, traefik, "service"), "spec")["type"])

		// Its own, non-default IngressClass.
		ic := subMap(t, traefik, "ingressClass")
		assert.Equal(t, "traefik-internal", ic["name"])
		assert.Equal(t, false, ic["isDefaultClass"])
	})

	t.Run("apiKey empty: both render empty", func(t *testing.T) {
		tv := forkTV("")
		for _, p := range []string{appTmpl, valuesTmpl} {
			rendered := templates.ParseAndExecuteTemplate(
				context.Background(), &KubeaidConfigFileTemplates, p, tv)
			assert.Empty(t, strings.TrimSpace(string(rendered)),
				"%s must render empty without netbird.apiKey", p)
		}
	})
}
