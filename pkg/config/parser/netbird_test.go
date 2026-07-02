// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package parser

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
)

func TestHydrateNetBirdDefaults(t *testing.T) {
	tests := []struct {
		name     string
		netbird  *config.NetBirdConfig
		wantStun string
		wantTurn string
		wantUser string
		wantZone string
	}{
		{
			name:    "nil block: no-op",
			netbird: nil,
		},
		{
			name:    "empty block: no-op (dnsZone NOT defaulted)",
			netbird: &config.NetBirdConfig{},
		},
		{
			name:     "netbird.<base> derives stun./turn. with same base",
			netbird:  &config.NetBirdConfig{DNS: "netbird.vpn.acme.com"},
			wantStun: "stun.vpn.acme.com",
			wantTurn: "turn.vpn.acme.com",
			wantUser: "netbird",
		},
		{
			name:     "non-netbird-prefix DNS: whole DNS becomes the base",
			netbird:  &config.NetBirdConfig{DNS: "mesh.acme.com"},
			wantStun: "stun.mesh.acme.com",
			wantTurn: "turn.mesh.acme.com",
			wantUser: "netbird",
		},
		{
			name:     "explicit dnsZone is left untouched (never derived/overwritten)",
			netbird:  &config.NetBirdConfig{DNS: "netbird.vpn.acme.com", DNSZone: "mesh.acme.com"},
			wantStun: "stun.vpn.acme.com",
			wantTurn: "turn.vpn.acme.com",
			wantUser: "netbird",
			wantZone: "mesh.acme.com",
		},
		{
			name:     "explicit StunDNS preserved",
			netbird:  &config.NetBirdConfig{DNS: "netbird.vpn.acme.com", StunDNS: "stun-custom.acme.com"},
			wantStun: "stun-custom.acme.com",
			wantTurn: "turn.vpn.acme.com",
			wantUser: "netbird",
		},
		{
			name:     "explicit TurnUser preserved",
			netbird:  &config.NetBirdConfig{DNS: "netbird.vpn.acme.com", TurnUser: "myturnuser"},
			wantStun: "stun.vpn.acme.com",
			wantTurn: "turn.vpn.acme.com",
			wantUser: "myturnuser",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			orig := config.ParsedGeneralConfig
			config.ParsedGeneralConfig = &config.GeneralConfig{}
			config.ParsedGeneralConfig.Cluster.NetBird = tc.netbird
			t.Cleanup(func() { config.ParsedGeneralConfig = orig })

			hydrateNetBirdDefaults()

			if tc.netbird == nil {
				assert.Nil(t, config.ParsedGeneralConfig.Cluster.NetBird)
				return
			}

			got := config.ParsedGeneralConfig.Cluster.NetBird
			assert.Equal(t, tc.wantStun, got.StunDNS)
			assert.Equal(t, tc.wantTurn, got.TurnDNS)
			assert.Equal(t, tc.wantUser, got.TurnUser)
			assert.Equal(t, tc.wantZone, got.DNSZone)
		})
	}
}

// TestHydrateNetBirdDefaultsClusterProxy covers clusterProxy.clusterName
// defaulting. Complements TestHydrateNetBirdDefaults, which covers
// stun/turn/turnUser. The network router's DNS zone is no longer derived
// here at all (operator-created in the NetBird dashboard instead), so
// there's nothing router-related left to default.
func TestHydrateNetBirdDefaultsClusterProxy(t *testing.T) {
	tests := []struct {
		name        string
		clusterName string
		netbird     *config.NetBirdConfig

		// wantNilProxy asserts hydrateNetBirdDefaults never allocates a
		// ClusterProxy block the operator didn't configure.
		wantNilProxy  bool
		wantProxyName string
	}{
		{
			name:         "nil NetBird block: no panic, no-op",
			clusterName:  "acme-prod",
			netbird:      nil,
			wantNilProxy: true,
		},
		{
			name:        "clusterProxy present, clusterName unset: defaults to cluster.name",
			clusterName: "acme-prod",
			netbird: &config.NetBirdConfig{
				DNS:          "netbird.vpn.acme.com",
				ClusterProxy: &config.NetBirdClusterProxyConfig{},
			},
			wantProxyName: "acme-prod",
		},
		{
			name:        "clusterProxy.clusterName already set: not overridden",
			clusterName: "acme-prod",
			netbird: &config.NetBirdConfig{
				DNS:          "netbird.vpn.acme.com",
				ClusterProxy: &config.NetBirdClusterProxyConfig{ClusterName: "custom-name"},
			},
			wantProxyName: "custom-name",
		},
		{
			name:        "cfg.DNS empty: clusterProxy.clusterName STILL derives from cluster.name",
			clusterName: "acme-prod",
			netbird: &config.NetBirdConfig{
				DNS:          "",
				ClusterProxy: &config.NetBirdClusterProxyConfig{},
			},
			wantProxyName: "acme-prod",
		},
		{
			name:        "clusterProxy absent: hydrate does not allocate it",
			clusterName: "acme-prod",
			netbird: &config.NetBirdConfig{
				DNS: "netbird.vpn.acme.com",
			},
			wantNilProxy: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			orig := config.ParsedGeneralConfig
			config.ParsedGeneralConfig = &config.GeneralConfig{}
			config.ParsedGeneralConfig.Cluster.Name = tc.clusterName
			config.ParsedGeneralConfig.Cluster.NetBird = tc.netbird
			t.Cleanup(func() { config.ParsedGeneralConfig = orig })

			hydrateNetBirdDefaults()

			if tc.netbird == nil {
				assert.Nil(t, config.ParsedGeneralConfig.Cluster.NetBird)
				return
			}

			got := config.ParsedGeneralConfig.Cluster.NetBird

			if tc.wantNilProxy {
				assert.Nil(t, got.ClusterProxy)
			} else {
				require.NotNil(t, got.ClusterProxy)
				assert.Equal(t, tc.wantProxyName, got.ClusterProxy.ClusterName)
			}
		})
	}
}
