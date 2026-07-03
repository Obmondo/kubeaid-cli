// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package netbird

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
)

// withFreshGeneralConfig swaps ParsedGeneralConfig for the duration of fn so
// the test never leaks the package-level config state to other tests.
func withFreshGeneralConfig(t *testing.T, fn func()) {
	t.Helper()

	orig := config.ParsedGeneralConfig
	config.ParsedGeneralConfig = &config.GeneralConfig{}

	t.Cleanup(func() { config.ParsedGeneralConfig = orig })

	fn()
}

func TestOperatorEnabled(t *testing.T) {
	tests := []struct {
		name        string
		clusterType string
		keycloak    *config.KeycloakConfig
		want        bool
	}{
		{
			name:        "workload + keycloak block: true",
			clusterType: constants.ClusterTypeWorkload,
			keycloak:    &config.KeycloakConfig{Mode: "external", DNS: "kc.acme.com"},
			want:        true,
		},
		{
			name:        "workload + no keycloak: false (admin.conf-only path)",
			clusterType: constants.ClusterTypeWorkload,
			keycloak:    nil,
			want:        false,
		},
		{
			// VPN clusters get the operator unconditionally — the cluster
			// itself runs NetBird Mgmt, so the operator's CRDs are how
			// routing-peer wiring gets declared.
			name:        "vpn cluster + managed keycloak: true",
			clusterType: constants.ClusterTypeVPN,
			keycloak:    &config.KeycloakConfig{Mode: "managed", DNS: "kc.acme.com"},
			want:        true,
		},
		{
			name:        "vpn cluster, no keycloak: true (nil-safe)",
			clusterType: constants.ClusterTypeVPN,
			keycloak:    nil,
			want:        true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			withFreshGeneralConfig(t, func() {
				config.ParsedGeneralConfig.Cluster.Type = tc.clusterType
				config.ParsedGeneralConfig.Cluster.Keycloak = tc.keycloak

				assert.Equal(t, tc.want, OperatorEnabled())
			})
		})
	}
}

func TestExpectedHost(t *testing.T) {
	cases := []struct {
		name        string
		keycloakDNS string
		want        string
	}{
		{"conventional keycloak.<base> name", "keycloak.vpn.acme.com", "netbird.vpn.acme.com"},
		{"deeper base is preserved", "keycloak.k8s.acme.io", "netbird.k8s.acme.io"},
		{"off-convention DNS yields empty (no guess)", "auth.acme.com", ""},
		{"empty DNS yields empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, ExpectedHost(tc.keycloakDNS))
		})
	}
}

func TestManagementURL(t *testing.T) {
	cases := []struct {
		name     string
		netbird  *config.NetBirdConfig
		keycloak *config.KeycloakConfig
		want     string
	}{
		{
			name:     "vpn cluster: cluster.netbird.dns is authoritative",
			netbird:  &config.NetBirdConfig{DNS: "netbird.vpn.acme.com"},
			keycloak: &config.KeycloakConfig{DNS: "keycloak.other.acme.com"},
			want:     "https://netbird.vpn.acme.com",
		},
		{
			name:     "workload: derived from the keycloak DNS convention",
			netbird:  nil,
			keycloak: &config.KeycloakConfig{DNS: "keycloak.vpn.acme.com"},
			want:     "https://netbird.vpn.acme.com",
		},
		{
			// The operator binary would fall back to NetBird Cloud; better
			// to render nothing and let the gate instructions cover it.
			name:     "workload, off-convention keycloak DNS: empty (no guess)",
			netbird:  nil,
			keycloak: &config.KeycloakConfig{DNS: "auth.acme.com"},
			want:     "",
		},
		{
			name:     "no keycloak block: empty",
			netbird:  nil,
			keycloak: nil,
			want:     "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withFreshGeneralConfig(t, func() {
				config.ParsedGeneralConfig.Cluster.NetBird = tc.netbird
				config.ParsedGeneralConfig.Cluster.Keycloak = tc.keycloak

				assert.Equal(t, tc.want, ManagementURL())
			})
		})
	}
}

func TestClusterProxyEnabled(t *testing.T) {
	tests := []struct {
		name    string
		netbird *config.NetBirdConfig
		want    bool
	}{
		{name: "nil NetBird block: false", netbird: nil, want: false},
		{name: "ClusterProxy block absent: false", netbird: &config.NetBirdConfig{}, want: false},
		{
			name:    "ClusterProxy present but disabled: false",
			netbird: &config.NetBirdConfig{ClusterProxy: &config.NetBirdClusterProxyConfig{Enabled: false}},
			want:    false,
		},
		{
			name:    "ClusterProxy present and enabled: true",
			netbird: &config.NetBirdConfig{ClusterProxy: &config.NetBirdClusterProxyConfig{Enabled: true}},
			want:    true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			withFreshGeneralConfig(t, func() {
				config.ParsedGeneralConfig.Cluster.NetBird = tc.netbird
				assert.Equal(t, tc.want, ClusterProxyEnabled())
			})
		})
	}
}
