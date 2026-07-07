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

	"github.com/Obmondo/kubeaid-cli/pkg/constants"
)

func TestNetBirdBlockEnabled(t *testing.T) {
	tests := []struct {
		name string
		cfg  PromptedConfig
		want bool
	}{
		{
			name: "vpn cluster always hosts netbird (even before the zone form)",
			cfg:  PromptedConfig{ClusterType: constants.ClusterTypeVPN},
			want: true,
		},
		{
			name: "workload joining a mesh has a zone",
			cfg: PromptedConfig{
				ClusterType:    constants.ClusterTypeWorkload,
				NetBirdDNSZone: "mesh.acme.com",
			},
			want: true,
		},
		{
			name: "workload not joining a mesh has no zone",
			cfg:  PromptedConfig{ClusterType: constants.ClusterTypeWorkload},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.cfg.NetBirdBlockEnabled())
		})
	}
}

// TestRenderWorkloadOmitsNetBirdWhenNotJoining proves a workload cluster that
// isn't on a NetBird mesh renders no cluster.netbird block at all — the whole
// block (and its dnsZone line) is gated, not just the optional dns line.
func TestRenderWorkloadOmitsNetBirdWhenNotJoining(t *testing.T) {
	cfg := &PromptedConfig{
		SSHUsername:                "git",
		UseSSHAgent:                true,
		KubeaidForkURL:             "https://github.com/Obmondo/kubeaid.git",
		KubeaidVersion:             "29.0.9",
		KubeaidConfigForkURL:       "git@github.com:acme/kubeaid-config.git",
		KubeaidConfigDeployKeyPath: "/tmp/ssh-priv",
		ClusterName:                "plain-workload",
		ClusterType:                constants.ClusterTypeWorkload,
		K8sVersion:                 "v1.35.4",
		CloudProvider:              constants.CloudProviderLocal,
		// No NetBird* fields — the cluster declined the mesh.
	}

	dir := t.TempDir()
	require.NoError(t, writeConfigFiles(dir, cfg))

	body, err := os.ReadFile(filepath.Join(dir, "general.yaml"))
	require.NoError(t, err)
	general := string(body)
	t.Logf("--- rendered general.yaml (workload, no netbird) ---\n%s", general)

	assert.NotContains(t, general, "netbird:", "no netbird block when not joining a mesh")
	assert.NotContains(t, general, "dnsZone:", "no dnsZone line when not joining a mesh")

	// The gated block must leave valid YAML with no cluster.netbird key.
	var parsed map[string]any
	require.NoError(t, yaml.Unmarshal(body, &parsed), "gated general.yaml must stay valid YAML")
	cluster, ok := parsed["cluster"].(map[string]any)
	require.True(t, ok, "cluster block must render")
	_, hasNetBird := cluster["netbird"]
	assert.False(t, hasNetBird, "cluster.netbird must be absent when not joining a mesh")
}
