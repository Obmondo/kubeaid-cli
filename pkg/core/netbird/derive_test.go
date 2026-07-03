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
		netbird     *config.NetBirdConfig
		want        bool
	}{
		{
			name:        "workload + netbird.dns set: true",
			clusterType: constants.ClusterTypeWorkload,
			netbird:     &config.NetBirdConfig{DNS: "netbird.vpn.acme.com"},
			want:        true,
		},
		{
			name:        "workload + netbird block but no dns: false",
			clusterType: constants.ClusterTypeWorkload,
			netbird:     &config.NetBirdConfig{DNSZone: "acme.local"},
			want:        false,
		},
		{
			name:        "workload + no netbird block: false (admin.conf-only path)",
			clusterType: constants.ClusterTypeWorkload,
			netbird:     nil,
			want:        false,
		},
		{
			// VPN clusters get the operator unconditionally — the cluster
			// itself runs NetBird Mgmt, so the operator's CRDs are how
			// routing-peer wiring gets declared.
			name:        "vpn cluster: true",
			clusterType: constants.ClusterTypeVPN,
			netbird:     &config.NetBirdConfig{DNS: "netbird.vpn.acme.com"},
			want:        true,
		},
		{
			name:        "vpn cluster, no netbird block: true (nil-safe)",
			clusterType: constants.ClusterTypeVPN,
			netbird:     nil,
			want:        true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			withFreshGeneralConfig(t, func() {
				config.ParsedGeneralConfig.Cluster.Type = tc.clusterType
				config.ParsedGeneralConfig.Cluster.NetBird = tc.netbird

				assert.Equal(t, tc.want, OperatorEnabled())
			})
		})
	}
}

func TestManagementURL(t *testing.T) {
	cases := []struct {
		name    string
		netbird *config.NetBirdConfig
		want    string
	}{
		{
			name:    "netbird.dns set: authoritative",
			netbird: &config.NetBirdConfig{DNS: "netbird.vpn.acme.com"},
			want:    "https://netbird.vpn.acme.com",
		},
		{
			name:    "netbird block but no dns: empty",
			netbird: &config.NetBirdConfig{DNSZone: "acme.local"},
			want:    "",
		},
		{
			name:    "no netbird block: empty",
			netbird: nil,
			want:    "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withFreshGeneralConfig(t, func() {
				config.ParsedGeneralConfig.Cluster.NetBird = tc.netbird

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
