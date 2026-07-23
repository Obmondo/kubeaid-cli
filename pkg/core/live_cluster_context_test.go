// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package core

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
)

// writeKubeconfig drops a kubeconfig on disk and returns its path.
func writeKubeconfig(t *testing.T, contents string) string {
	t.Helper()

	kubeconfigPath := filepath.Join(t.TempDir(), "kubeconfig.yaml")
	require.NoError(t, os.WriteFile(kubeconfigPath, []byte(contents), 0o600))

	return kubeconfigPath
}

const validKubeconfig = `apiVersion: v1
kind: Config
current-context: netbird-acme-com
clusters:
  - name: netbird-acme-com
    cluster:
      server: https://api.vpn.acme.com:6443
contexts:
  - name: netbird-acme-com
    context:
      cluster: netbird-acme-com
      user: netbird
users:
  - name: netbird
    user: {}
`

func TestDescribeLiveClusterContext(t *testing.T) {
	t.Parallel()

	t.Run("resolves the current-context into its cluster and server", func(t *testing.T) {
		t.Parallel()

		live, err := describeLiveClusterContext(writeKubeconfig(t, validKubeconfig))
		require.NoError(t, err)

		assert.Equal(t, "netbird-acme-com", live.ContextName)
		assert.Equal(t, "netbird-acme-com", live.ClusterName)
		assert.Equal(t, "https://api.vpn.acme.com:6443", live.Server)
	})

	t.Run("fails when the kubeconfig is missing", func(t *testing.T) {
		t.Parallel()

		_, err := describeLiveClusterContext(filepath.Join(t.TempDir(), "absent.yaml"))
		require.Error(t, err)
	})

	t.Run("fails when no current-context is set", func(t *testing.T) {
		t.Parallel()

		// Without this, every client would silently fall back to an empty context.
		_, err := describeLiveClusterContext(writeKubeconfig(t, `apiVersion: v1
kind: Config
clusters: []
contexts: []
users: []
`))
		require.ErrorContains(t, err, "no current-context")
	})

	t.Run("fails when current-context names a context that is not defined", func(t *testing.T) {
		t.Parallel()

		_, err := describeLiveClusterContext(writeKubeconfig(t, `apiVersion: v1
kind: Config
current-context: ghost
clusters: []
contexts: []
users: []
`))
		require.ErrorContains(t, err, "doesn't define")
	})
}

func TestLiveClusterContextMatchesConfiguredCluster(t *testing.T) {
	// Mutates the config package global, so it cannot run in parallel with other tests
	// that read it.
	original := config.ParsedGeneralConfig
	t.Cleanup(func() { config.ParsedGeneralConfig = original })

	live, err := describeLiveClusterContext(writeKubeconfig(t, validKubeconfig))
	require.NoError(t, err)

	config.ParsedGeneralConfig.Cluster.Name = "netbird-acme-com"
	assert.True(t, live.matchesConfiguredCluster())
	assert.NotContains(t, live.summary("sync"), "MISMATCH")

	// The case the gate exists for: a kubeconfig left pointing at another cluster.
	config.ParsedGeneralConfig.Cluster.Name = "kbm-acme-com"
	assert.False(t, live.matchesConfiguredCluster())

	summary := live.summary("sync")
	assert.Contains(t, summary, "MISMATCH")
	assert.Contains(t, summary, "netbird-acme-com")
	assert.Contains(t, summary, "kbm-acme-com")
}

// The chart gates template rotation on global.machineTemplateRotation, so the rendered
// values file has to carry it. Without this line a cluster silently keeps the fixed
// template names and no instance-type change ever rolls.
func TestCapiClusterValuesRendersMachineTemplateRotation(t *testing.T) {
	t.Parallel()

	const tmplPath = "templates/argocd-apps/values-capi-cluster.yaml.tmpl"

	globalOf := func(t *testing.T, out string) map[string]any {
		t.Helper()

		var parsed map[string]any
		require.NoError(t, yaml.Unmarshal([]byte(out), &parsed), "rendered values must be valid YAML:\n%s", out)

		global, ok := parsed["global"].(map[string]any)
		require.True(t, ok, "global block must be present")

		return global
	}

	t.Run("defaults to false", func(t *testing.T) {
		t.Parallel()

		out := renderEmbeddedTemplate(t, tmplPath, capiClusterTV(false))
		assert.Equal(t, false, globalOf(t, out)["machineTemplateRotation"])
	})

	t.Run("renders true when enabled in general.yaml", func(t *testing.T) {
		t.Parallel()

		tv := capiClusterTV(false)
		tv.MachineTemplateRotation = true

		out := renderEmbeddedTemplate(t, tmplPath, tv)
		assert.Equal(t, true, globalOf(t, out)["machineTemplateRotation"])
	})
}
