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

// TestHydrateNetBirdDefaultsNetworkRouterAndClusterProxy covers
// networkRouter.dnsZone/replicas and clusterProxy.clusterName defaulting.
// Complements TestHydrateNetBirdDefaults, which covers stun/turn/turnUser.
func TestHydrateNetBirdDefaultsNetworkRouterAndClusterProxy(t *testing.T) {
	tests := []struct {
		name        string
		clusterName string
		netbird     *config.NetBirdConfig

		// wantNilRouter / wantNilProxy assert hydrateNetBirdDefaults never
		// allocates a block the operator didn't configure.
		wantNilRouter  bool
		wantRouterZone string
		wantRouterReps int

		wantNilProxy  bool
		wantProxyName string
	}{
		{
			name:          "nil NetBird block: no panic, no-op",
			clusterName:   "acme-prod",
			netbird:       nil,
			wantNilRouter: true,
			wantNilProxy:  true,
		},
		{
			name:        "networkRouter present, both fields unset: dnsZone derived from netbird DNS base, replicas defaults to 1",
			clusterName: "acme-prod",
			netbird: &config.NetBirdConfig{
				DNS:           "netbird.vpn.acme.com",
				NetworkRouter: &config.NetBirdNetworkRouterConfig{},
			},
			wantRouterZone: "vpn.acme.com",
			wantRouterReps: 1,
			wantNilProxy:   true,
		},
		{
			name:        "networkRouter.dnsZone already set: not overridden",
			clusterName: "acme-prod",
			netbird: &config.NetBirdConfig{
				DNS:           "netbird.vpn.acme.com",
				NetworkRouter: &config.NetBirdNetworkRouterConfig{DNSZone: "custom.acme.com"},
			},
			wantRouterZone: "custom.acme.com",
			wantRouterReps: 1, // still defaulted — Replicas was left at 0
			wantNilProxy:   true,
		},
		{
			name:        "networkRouter.replicas already set: not overridden",
			clusterName: "acme-prod",
			netbird: &config.NetBirdConfig{
				DNS:           "netbird.vpn.acme.com",
				NetworkRouter: &config.NetBirdNetworkRouterConfig{Replicas: 5},
			},
			wantRouterZone: "vpn.acme.com", // still derived — DNSZone was left empty
			wantRouterReps: 5,
			wantNilProxy:   true,
		},
		{
			name:        "cfg.DNS empty: networkRouter.dnsZone NOT derived, but replicas STILL defaults to 1",
			clusterName: "acme-prod",
			netbird: &config.NetBirdConfig{
				DNS:           "",
				NetworkRouter: &config.NetBirdNetworkRouterConfig{},
			},
			wantRouterZone: "",
			wantRouterReps: 1,
			wantNilProxy:   true,
		},
		{
			name:        "clusterProxy present, clusterName unset: defaults to cluster.name",
			clusterName: "acme-prod",
			netbird: &config.NetBirdConfig{
				DNS:          "netbird.vpn.acme.com",
				ClusterProxy: &config.NetBirdClusterProxyConfig{},
			},
			wantNilRouter: true,
			wantProxyName: "acme-prod",
		},
		{
			name:        "clusterProxy.clusterName already set: not overridden",
			clusterName: "acme-prod",
			netbird: &config.NetBirdConfig{
				DNS:          "netbird.vpn.acme.com",
				ClusterProxy: &config.NetBirdClusterProxyConfig{ClusterName: "custom-name"},
			},
			wantNilRouter: true,
			wantProxyName: "custom-name",
		},
		{
			name:        "cfg.DNS empty: clusterProxy.clusterName STILL derives from cluster.name",
			clusterName: "acme-prod",
			netbird: &config.NetBirdConfig{
				DNS:          "",
				ClusterProxy: &config.NetBirdClusterProxyConfig{},
			},
			wantNilRouter: true,
			wantProxyName: "acme-prod",
		},
		{
			name:        "cfg.DNS empty, both blocks present: dnsZone empty, replicas + clusterName still derived",
			clusterName: "acme-prod",
			netbird: &config.NetBirdConfig{
				DNS:           "",
				NetworkRouter: &config.NetBirdNetworkRouterConfig{},
				ClusterProxy:  &config.NetBirdClusterProxyConfig{},
			},
			wantRouterZone: "",
			wantRouterReps: 1,
			wantProxyName:  "acme-prod",
		},
		{
			name:        "networkRouter and clusterProxy both absent: hydrate does not allocate them",
			clusterName: "acme-prod",
			netbird: &config.NetBirdConfig{
				DNS: "netbird.vpn.acme.com",
			},
			wantNilRouter: true,
			wantNilProxy:  true,
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

			if tc.wantNilRouter {
				assert.Nil(t, got.NetworkRouter)
			} else {
				require.NotNil(t, got.NetworkRouter)
				assert.Equal(t, tc.wantRouterZone, got.NetworkRouter.DNSZone)
				assert.Equal(t, tc.wantRouterReps, got.NetworkRouter.Replicas)
			}

			if tc.wantNilProxy {
				assert.Nil(t, got.ClusterProxy)
			} else {
				require.NotNil(t, got.ClusterProxy)
				assert.Equal(t, tc.wantProxyName, got.ClusterProxy.ClusterName)
			}
		})
	}
}
