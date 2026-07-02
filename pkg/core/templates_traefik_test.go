// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package core

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	"github.com/Obmondo/kubeaid-cli/pkg/utils/templates"
)

// renderTraefikValues renders values-traefik.yaml.tmpl via the production
// template pipeline and parses the result into a generic map.
func renderTraefikValues(t *testing.T, tv *TemplateValues) map[string]any {
	t.Helper()

	const tmplPath = "templates/argocd-apps/values-traefik.yaml.tmpl"
	rendered := templates.ParseAndExecuteTemplate(
		context.Background(), &KubeaidConfigFileTemplates, tmplPath, tv,
	)

	var parsed map[string]any
	require.NoError(t, yaml.Unmarshal(rendered, &parsed),
		"rendered output must be valid YAML:\n%s", rendered)

	return parsed
}

// TestTraefikValuesTemplate_InternalTraefikBlock covers the
// traefik-internal: {enabled: true} block, gated on NetBirdAPIKey being
// set — without a Mgmt key there's no mesh to expose the internal Traefik
// instance on.
func TestTraefikValuesTemplate_InternalTraefikBlock(t *testing.T) {
	t.Run("NetBirdAPIKey set: traefik-internal is enabled", func(t *testing.T) {
		parsed := renderTraefikValues(t, &TemplateValues{NetBirdAPIKey: "nb-mgmt-token"})

		require.Contains(t, parsed, "traefik-internal")
		internal := subMap(t, parsed, "traefik-internal")
		assert.Equal(t, true, internal["enabled"])

		// The always-on traefik: block must still render alongside it.
		assert.Contains(t, parsed, "traefik")
	})

	t.Run("NetBirdAPIKey empty: no traefik-internal key", func(t *testing.T) {
		parsed := renderTraefikValues(t, &TemplateValues{})

		assert.NotContains(t, parsed, "traefik-internal")
		assert.Contains(t, parsed, "traefik", "the base traefik: block must still render")
	})
}
