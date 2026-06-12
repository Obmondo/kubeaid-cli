// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package parser

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
)

func TestHydrateNetBirdDefaults(t *testing.T) {
	tests := []struct {
		name        string
		clusterName string
		netbird     *config.NetBirdConfig
		wantStun    string
		wantTurn    string
		wantUser    string
		wantZone    string
	}{
		{
			name:    "nil block: no-op",
			netbird: nil,
		},
		{
			name:        "empty block: dnsZone defaults to <cluster>.local, rest untouched",
			clusterName: "kbm-obmondo-com",
			netbird:     &config.NetBirdConfig{},
			wantZone:    "kbm-obmondo-com.local",
		},
		{
			name:        "netbird.<base> derives stun./turn.; dnsZone defaults",
			clusterName: "staging",
			netbird:     &config.NetBirdConfig{DNS: "netbird.vpn.acme.com"},
			wantStun:    "stun.vpn.acme.com",
			wantTurn:    "turn.vpn.acme.com",
			wantUser:    "netbird",
			wantZone:    "staging.local",
		},
		{
			name:        "non-netbird-prefix DNS: whole DNS becomes the base",
			clusterName: "staging",
			netbird:     &config.NetBirdConfig{DNS: "mesh.acme.com"},
			wantStun:    "stun.mesh.acme.com",
			wantTurn:    "turn.mesh.acme.com",
			wantUser:    "netbird",
			wantZone:    "staging.local",
		},
		{
			name:        "explicit dnsZone preserved",
			clusterName: "staging",
			netbird:     &config.NetBirdConfig{DNSZone: "mesh.acme.internal"},
			wantZone:    "mesh.acme.internal",
		},
		{
			name:        "explicit StunDNS preserved",
			clusterName: "staging",
			netbird:     &config.NetBirdConfig{DNS: "netbird.vpn.acme.com", StunDNS: "stun-custom.acme.com"},
			wantStun:    "stun-custom.acme.com",
			wantTurn:    "turn.vpn.acme.com",
			wantUser:    "netbird",
			wantZone:    "staging.local",
		},
		{
			name:        "explicit TurnDNS preserved",
			clusterName: "staging",
			netbird:     &config.NetBirdConfig{DNS: "netbird.vpn.acme.com", TurnDNS: "turn-custom.acme.com"},
			wantStun:    "stun.vpn.acme.com",
			wantTurn:    "turn-custom.acme.com",
			wantUser:    "netbird",
			wantZone:    "staging.local",
		},
		{
			name:        "explicit TurnUser preserved",
			clusterName: "staging",
			netbird:     &config.NetBirdConfig{DNS: "netbird.vpn.acme.com", TurnUser: "myturnuser"},
			wantStun:    "stun.vpn.acme.com",
			wantTurn:    "turn.vpn.acme.com",
			wantUser:    "myturnuser",
			wantZone:    "staging.local",
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
			assert.Equal(t, tc.wantStun, got.StunDNS)
			assert.Equal(t, tc.wantTurn, got.TurnDNS)
			assert.Equal(t, tc.wantUser, got.TurnUser)
			assert.Equal(t, tc.wantZone, got.DNSZone)
		})
	}
}

func TestDefaultNetBirdDNSZone(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "kbm-obmondo-com.local", DefaultNetBirdDNSZone("kbm-obmondo-com"))
	assert.Equal(t, "staging.local", DefaultNetBirdDNSZone("staging"))
}
