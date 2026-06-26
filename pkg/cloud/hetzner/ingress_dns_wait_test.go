// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package hetzner

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
)

// TestIngressLBFQDNsCoturnFloatingIPSplit pins the commit's core
// behaviour: when a multi-CP HCloud VPN cluster has a Coturn Floating IP,
// stun/turn move OUT of the Traefik-LB FQDN set (they must resolve to the
// Floating IP instead); without a Floating IP they stay on the LB.
//
// Mutates config.ParsedGeneralConfig — sequential only.
func TestIngressLBFQDNsCoturnFloatingIPSplit(t *testing.T) {
	setup := func(t *testing.T, cfg *config.GeneralConfig) {
		t.Helper()
		saved := config.ParsedGeneralConfig
		t.Cleanup(func() { config.ParsedGeneralConfig = saved })
		config.ParsedGeneralConfig = cfg
	}

	vpnHCloudCfg := func(cpReplicas uint) *config.GeneralConfig {
		return &config.GeneralConfig{
			Cluster: config.ClusterConfig{
				Type:     constants.ClusterTypeVPN,
				Keycloak: &config.KeycloakConfig{DNS: "keycloak.vpn.acme.com"},
				NetBird: &config.NetBirdConfig{
					DNS:     "netbird.vpn.acme.com",
					StunDNS: "stun.vpn.acme.com",
					TurnDNS: "turn.vpn.acme.com",
				},
			},
			Cloud: config.CloudConfig{
				Hetzner: &config.HetznerConfig{
					Mode: constants.HetznerModeHCloud,
					ControlPlane: config.HetznerControlPlane{
						HCloud: &config.HCloudControlPlane{Replicas: cpReplicas},
					},
				},
			},
		}
	}

	t.Run("multi-CP (FIP): stun/turn split out of the LB set onto the Floating IP", func(t *testing.T) {
		setup(t, vpnHCloudCfg(3))

		lbFQDNs := ingressLBFQDNs()
		assert.Contains(t, lbFQDNs, "keycloak.vpn.acme.com")
		assert.Contains(t, lbFQDNs, "netbird.vpn.acme.com")
		assert.NotContains(t, lbFQDNs, "stun.vpn.acme.com")
		assert.NotContains(t, lbFQDNs, "turn.vpn.acme.com")

		assert.Equal(t,
			[]string{"stun.vpn.acme.com", "turn.vpn.acme.com"},
			coturnFloatingIPFQDNs(),
		)
	})

	t.Run("single-CP (no FIP): stun/turn stay on the Traefik LB", func(t *testing.T) {
		setup(t, vpnHCloudCfg(1))

		lbFQDNs := ingressLBFQDNs()
		assert.Contains(t, lbFQDNs, "stun.vpn.acme.com")
		assert.Contains(t, lbFQDNs, "turn.vpn.acme.com")

		assert.Nil(t, coturnFloatingIPFQDNs())
	})
}
