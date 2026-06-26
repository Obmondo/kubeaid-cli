// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package core

import (
	"bytes"
	"testing"
	"text/template"

	"github.com/go-sprout/sprout"
	"github.com/go-sprout/sprout/registry/encoding"
	sproutstrings "github.com/go-sprout/sprout/registry/strings"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
)

// renderEmbeddedTemplate executes an embedded kubeaid-config template
// against the given TemplateValues, mirroring the sprout FuncMap used in
// production (encoding + strings registries).
func renderEmbeddedTemplate(t *testing.T, tmplPath string, values TemplateValues) string {
	t.Helper()

	raw, err := KubeaidConfigFileTemplates.ReadFile(tmplPath)
	require.NoError(t, err)

	sproutFuncs := sprout.New(sprout.WithRegistries(
		encoding.NewRegistry(),
		sproutstrings.NewRegistry(),
	)).Build()

	tmpl, err := template.New(tmplPath).Funcs(sproutFuncs).Parse(string(raw))
	require.NoError(t, err)

	var buf bytes.Buffer
	require.NoError(t, tmpl.Execute(&buf, values))
	return buf.String()
}

// TestHCloudFIPControllerValuesOverlay verifies the controller's values
// overlay renders the provisioned Floating IP + the token-secret ref, and
// renders nothing on a cluster without a Coturn Floating IP.
func TestHCloudFIPControllerValuesOverlay(t *testing.T) {
	const tmplPath = "templates/argocd-apps/values-hcloud-fip-controller.yaml.tmpl"

	t.Run("renders floatingIPs and existingSecretName when a Floating IP is set", func(t *testing.T) {
		out := renderEmbeddedTemplate(t, tmplPath, TemplateValues{
			CoturnFloatingIPs: []string{"203.0.113.10"},
		})
		assert.Contains(t, out, "hcloud-fip-controller:")
		assert.Contains(t, out, "- 203.0.113.10")
		assert.Contains(t, out, "existingSecretName: hcloud-fip-controller-token")
	})

	t.Run("renders nothing when no Floating IP is set", func(t *testing.T) {
		out := renderEmbeddedTemplate(t, tmplPath, TemplateValues{})
		assert.NotContains(t, out, "hcloud-fip-controller:")
		assert.NotContains(t, out, "floatingIPs")
	})
}

// TestHCloudFIPControllerTokenSecret verifies the token Secret carries the
// hetzner API token under exactly the HCLOUD_API_TOKEN key the chart's
// envFrom.secretRef reads, in kube-system.
func TestHCloudFIPControllerTokenSecret(t *testing.T) {
	const tmplPath = "templates/sealed-secrets/hcloud-fip-controller/token.yaml.tmpl"

	out := renderEmbeddedTemplate(t, tmplPath, TemplateValues{
		HetznerCredentials: &config.HetznerCredentials{APIToken: "hcloud-token-xyz"},
	})
	assert.Contains(t, out, "name: hcloud-fip-controller-token")
	assert.Contains(t, out, "namespace: kube-system")
	assert.Contains(t, out, "HCLOUD_API_TOKEN:")
	assert.Contains(t, out, "hcloud-token-xyz")
}
