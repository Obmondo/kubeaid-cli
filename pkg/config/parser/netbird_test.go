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
