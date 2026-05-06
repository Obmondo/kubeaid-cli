// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package parser

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
)

func TestHydrateNetBirdDefaults(t *testing.T) {
	tests := []struct {
		name     string
		netbird  *config.NetBirdConfig
		wantStun string
		wantTurn string
		wantUser string
	}{
		{
			name:     "nil block: no-op",
			netbird:  nil,
			wantStun: "",
			wantTurn: "",
			wantUser: "",
		},
		{
			name:     "empty DNS: no-op",
			netbird:  &config.NetBirdConfig{},
			wantStun: "",
			wantTurn: "",
			wantUser: "",
		},
		{
			name: "netbird.<base> derives stun./turn. with same base",
			netbird: &config.NetBirdConfig{
				DNS: "netbird.vpn.acme.com",
			},
			wantStun: "stun.vpn.acme.com",
			wantTurn: "turn.vpn.acme.com",
			wantUser: "netbird",
		},
		{
			name: "non-netbird-prefix DNS: whole DNS becomes the base",
			netbird: &config.NetBirdConfig{
				DNS: "mesh.acme.com",
			},
			wantStun: "stun.mesh.acme.com",
			wantTurn: "turn.mesh.acme.com",
			wantUser: "netbird",
		},
		{
			name: "explicit StunDNS preserved",
			netbird: &config.NetBirdConfig{
				DNS:     "netbird.vpn.acme.com",
				StunDNS: "stun-custom.acme.com",
			},
			wantStun: "stun-custom.acme.com",
			wantTurn: "turn.vpn.acme.com",
			wantUser: "netbird",
		},
		{
			name: "explicit TurnDNS preserved",
			netbird: &config.NetBirdConfig{
				DNS:     "netbird.vpn.acme.com",
				TurnDNS: "turn-custom.acme.com",
			},
			wantStun: "stun.vpn.acme.com",
			wantTurn: "turn-custom.acme.com",
			wantUser: "netbird",
		},
		{
			name: "explicit TurnUser preserved",
			netbird: &config.NetBirdConfig{
				DNS:      "netbird.vpn.acme.com",
				TurnUser: "myturnuser",
			},
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

			assert.Equal(t, tc.wantStun, config.ParsedGeneralConfig.Cluster.NetBird.StunDNS)
			assert.Equal(t, tc.wantTurn, config.ParsedGeneralConfig.Cluster.NetBird.TurnDNS)
			assert.Equal(t, tc.wantUser, config.ParsedGeneralConfig.Cluster.NetBird.TurnUser)
		})
	}
}
