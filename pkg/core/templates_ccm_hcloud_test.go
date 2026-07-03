// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package core

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
)

// TestCCMHCloudValuesTemplateLoadBalancerEnv exercises the
// HCLOUD_LOAD_BALANCERS_DISABLE_PUBLIC_NETWORK / HCLOUD_LOAD_BALANCERS_USE_PRIVATE_IP
// gates in values-ccm-hcloud.yaml.tmpl.
//
// HCloud nodes get no public IPv4/IPv6 on either a vpn-type cluster or a
// workload cluster fronted by a VPN (HCloudMachineTemplate.yaml's
// network.type=private default applies to both), so USE_PRIVATE_IP — which
// controls how ccm-hcloud attaches LB *backend* targets — must render for
// both. DISABLE_PUBLIC_NETWORK controls the LB *frontend* and stays
// workload-behind-VPN only: a vpn-type cluster hosts its own public-facing
// LBs (Keycloak / NetBird ingress).
func TestCCMHCloudValuesTemplateLoadBalancerEnv(t *testing.T) {
	const tmplPath = "templates/argocd-apps/values-ccm-hcloud.yaml.tmpl"

	tests := []struct {
		name            string
		tmplValues      TemplateValues
		wantContains    []string
		wantNotContains []string
	}{
		{
			name: "vpn-type cluster: USE_PRIVATE_IP + networking set, DISABLE_PUBLIC_NETWORK not set",
			tmplValues: TemplateValues{
				ClusterConfig: config.ClusterConfig{Type: constants.ClusterTypeVPN},
				HetznerConfig: &config.HetznerConfig{
					HCloud: &config.HCloudConfig{Zone: "eu-central"},
				},
			},
			wantContains: []string{
				"HCLOUD_LOAD_BALANCERS_USE_PRIVATE_IP:",
				"networking:",
			},
			wantNotContains: []string{
				"HCLOUD_LOAD_BALANCERS_DISABLE_PUBLIC_NETWORK:",
			},
		},
		{
			// Single public control-plane node: no private network, so the CCM
			// must not attach LB targets over a private IP and must not manage
			// private-network pod routes. Both USE_PRIVATE_IP and networking
			// drop out; the node's InternalIP comes from its public interface.
			name: "single-node public: no USE_PRIVATE_IP, no networking",
			tmplValues: TemplateValues{
				ClusterConfig:          config.ClusterConfig{Type: constants.ClusterTypeVPN},
				HCloudSingleNodePublic: true,
				HetznerConfig: &config.HetznerConfig{
					HCloud: &config.HCloudConfig{Zone: "eu-central"},
				},
			},
			wantContains: []string{
				// Baseline env still renders regardless of topology.
				"HCLOUD_LOAD_BALANCERS_NETWORK_ZONE:",
			},
			wantNotContains: []string{
				"HCLOUD_LOAD_BALANCERS_USE_PRIVATE_IP:",
				"HCLOUD_LOAD_BALANCERS_DISABLE_PUBLIC_NETWORK:",
				"networking:",
			},
		},
		{
			name: "workload cluster fronted by VPN: both env vars set",
			tmplValues: TemplateValues{
				ClusterConfig: config.ClusterConfig{Type: constants.ClusterTypeWorkload},
				HetznerConfig: &config.HetznerConfig{
					HCloud:           &config.HCloudConfig{Zone: "eu-central"},
					HCloudVPNCluster: &config.HCloudVPNClusterConfig{Name: "vpn-cluster"},
				},
			},
			wantContains: []string{
				"HCLOUD_LOAD_BALANCERS_USE_PRIVATE_IP:",
				"HCLOUD_LOAD_BALANCERS_DISABLE_PUBLIC_NETWORK:",
			},
		},
		{
			name: "standalone workload cluster (no VPN): neither env var set",
			tmplValues: TemplateValues{
				ClusterConfig: config.ClusterConfig{Type: constants.ClusterTypeWorkload},
				HetznerConfig: &config.HetznerConfig{
					HCloud: &config.HCloudConfig{Zone: "eu-central"},
				},
			},
			wantNotContains: []string{
				"HCLOUD_LOAD_BALANCERS_USE_PRIVATE_IP:",
				"HCLOUD_LOAD_BALANCERS_DISABLE_PUBLIC_NETWORK:",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rendered := renderEmbeddedTemplate(t, tmplPath, tc.tmplValues)

			for _, want := range tc.wantContains {
				assert.Contains(t, rendered, want, "expected %q in rendered output", want)
			}
			for _, notWant := range tc.wantNotContains {
				assert.NotContains(t, rendered, notWant, "expected %q absent from rendered output", notWant)
			}
		})
	}
}
