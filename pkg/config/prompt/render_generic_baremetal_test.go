// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package prompt

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
)

// TestRenderGenericBareMetalWorkload proves the prompt flow renders actual
// control-plane / worker hosts for the generic bare-metal provider — the
// regression where general.yaml carried `hosts: []` (and the cluster name as
// the API endpoint), which sailed through validation and only failed minutes
// later inside KubeOne's own manifest validation.
func TestRenderGenericBareMetalWorkload(t *testing.T) {
	cfg := &PromptedConfig{
		SSHUsername:                "git",
		UseSSHAgent:                true,
		KubeaidForkURL:             "https://github.com/Obmondo/kubeaid.git",
		KubeaidVersion:             "31.0.4",
		KubeaidConfigForkURL:       "git@github.com:demo/kubeaid-config.git",
		ClusterName:                "demo",
		ClusterType:                "workload",
		K8sVersion:                 "v1.35.6",
		KubeaidConfigDeployKeyPath: "/tmp/ssh-priv",

		CloudProvider:              "bare-metal",
		BareMetalSSHPort:           "22",
		BareMetalEndpointHost:      "192.0.2.10",
		BareMetalEndpointPort:      "6443",
		BareMetalControlPlaneHosts: []string{"192.0.2.10", "192.0.2.11"},
		BareMetalWorkerHosts:       []string{"192.0.2.20"},
		BareMetalNodeGroupName:     "workers",
	}

	dir := t.TempDir()
	require.NoError(t, writeConfigFiles(dir, cfg))

	body, err := os.ReadFile(filepath.Join(dir, "general.yaml"))
	require.NoError(t, err)
	general := string(body)
	t.Logf("--- rendered general.yaml ---\n%s", general)

	assert.NotContains(t, general, "hosts: []",
		"bare-metal render must not emit an empty control-plane host list")
	assert.Contains(t, general, "host: 192.0.2.10")
	assert.Contains(t, general, "- publicAddress: 192.0.2.10")
	assert.Contains(t, general, "- publicAddress: 192.0.2.11")
	assert.Contains(t, general, "name: workers")
	assert.Contains(t, general, "- publicAddress: 192.0.2.20")

	parsed := &config.GeneralConfig{}
	//nolint:musttag // GeneralConfig has hydrated runtime fields without yaml tags by design — same waiver as pkg/config/parser/parse.go.
	require.NoError(t, yaml.Unmarshal(body, parsed),
		"rendered general.yaml should unmarshal cleanly")

	require.NotNil(t, parsed.Cloud.BareMetal, "rendered YAML must include cloud.bare-metal")
	assert.Len(t, parsed.Cloud.BareMetal.ControlPlane.Hosts, 2)
	require.Len(t, parsed.Cloud.BareMetal.NodeGroups, 1)
	assert.Len(t, parsed.Cloud.BareMetal.NodeGroups[0].Hosts, 1)
}
