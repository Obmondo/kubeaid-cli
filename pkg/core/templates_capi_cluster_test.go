// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
)

// capiClusterTV returns a TemplateValues for an HCloud VPN cluster with a
// single control-plane replica, ready to render values-capi-cluster.yaml.tmpl.
// singleNodePublic toggles the single-node public-control-plane topology; on
// the normal path a pre-created control-plane LB private IP is supplied.
func capiClusterTV(singleNodePublic bool) TemplateValues {
	tv := TemplateValues{
		ClusterConfig: config.ClusterConfig{
			Name: "demo", Type: constants.ClusterTypeVPN, K8sVersion: "v1.31.0",
		},
		HCloudSingleNodePublic: singleNodePublic,
		ControlPlaneEndpoint:   "api.demo.example.com",
		HetznerConfig: &config.HetznerConfig{
			Mode:       constants.HetznerModeHCloud,
			SSHKeyPair: config.HetznerSSHKeyPair{Name: "demo"},
			HCloud: &config.HCloudConfig{
				Zone:      "eu-central",
				ImageName: "ubuntu-24.04",
				HetznerNetwork: config.HetznerNetworkConfig{
					CIDR: "10.0.0.0/16", HCloudServersSubnetCIDR: "10.0.0.0/24",
				},
			},
			ControlPlane: config.HetznerControlPlane{
				HCloud: &config.HCloudControlPlane{
					MachineType: "cpx41",
					Replicas:    1,
					LoadBalancer: config.HCloudControlPlaneLoadBalancer{
						Region: "hel1", Endpoint: "api.demo.example.com",
					},
				},
			},
		},
	}
	if !singleNodePublic {
		tv.ControlPlaneLBPrivateIP = "10.0.0.5"
	}
	tv.KubeaidFork = config.KubeAidForkConfig{URL: "https://example.test/KubeAid.git", Version: "master"}
	tv.KubeaidConfigFork = config.KubeaidConfigForkConfig{URL: "https://example.test/cfg.git", Directory: "demo"}
	return tv
}

// TestCapiClusterValuesSingleNodePublic verifies the capi-cluster values
// overlay puts the lone control-plane node on a public network with no LB:
// network.type=public, the apiserver endpoint sourced from the api DNS name,
// loadBalancer.enabled=false, and no pre-created LB private IP block. The
// pre-created-LB path (vpn / workload-behind-VPN) must be unaffected.
func TestCapiClusterValuesSingleNodePublic(t *testing.T) {
	const tmplPath = "templates/argocd-apps/values-capi-cluster.yaml.tmpl"

	t.Run("single-node public: network.type=public, endpoint from DNS, no LB privateIP", func(t *testing.T) {
		out := renderEmbeddedTemplate(t, tmplPath, capiClusterTV(true))

		// Parse the output so indentation bugs (which a string match would
		// miss) surface as a structural mismatch or an unmarshal error.
		var parsed map[string]any
		require.NoError(t, yaml.Unmarshal([]byte(out), &parsed),
			"rendered capi-cluster values must be valid YAML:\n%s", out)
		hetzner, ok := parsed["hetzner"].(map[string]any)
		require.True(t, ok, "hetzner block must be present")

		network, ok := hetzner["network"].(map[string]any)
		require.True(t, ok, "hetzner.network must render for the public topology")
		assert.Equal(t, "public", network["type"],
			"the CP node must render on a public network so it gets a public IPv4")

		controlPlane, ok := hetzner["controlPlane"].(map[string]any)
		require.True(t, ok, "hetzner.controlPlane must be present")
		endpoint, ok := controlPlane["endpoint"].(map[string]any)
		require.True(t, ok, "controlPlane.endpoint must render from the api DNS name")
		assert.Equal(t, "api.demo.example.com", endpoint["host"])
		assert.NotContains(t, controlPlane, "loadBalancer",
			"there is no pre-created LB, so no controlPlane.loadBalancer block must render")

		hcloud, ok := controlPlane["hcloud"].(map[string]any)
		require.True(t, ok, "controlPlane.hcloud must be present")
		lb, ok := hcloud["loadBalancer"].(map[string]any)
		require.True(t, ok, "controlPlane.hcloud.loadBalancer must be present")
		assert.Equal(t, false, lb["enabled"], "CAPH must not create its own control-plane LB")
	})

	t.Run("normal VPN with pre-created LB: privateIP present, network.type absent", func(t *testing.T) {
		out := renderEmbeddedTemplate(t, tmplPath, capiClusterTV(false))

		assert.Contains(t, out, "privateIP: 10.0.0.5",
			"the pre-created LB private IP must render on the normal VPN path")
		assert.NotContains(t, out, "type: public",
			"the normal VPN path stays on the chart's default private network")
	})
}
