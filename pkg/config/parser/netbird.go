// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package parser

import (
	"strings"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
)

// hydrateNetBirdDefaults derives stun / turn DNS names and the static
// turn username when the operator hasn't set them explicitly.
//
// Default derivation:
//
//	netbird.<base>  → stun.<base> / turn.<base>
//
// where <base> is what's left after stripping the "netbird." prefix
// from cluster.netbird.dns. Mirrors the pattern operators use today
// (netbird.vpn.acme.com / stun.vpn.acme.com / turn.vpn.acme.com).
//
// When DNS doesn't start with "netbird." the whole DNS becomes the
// base — operators using a non-conventional naming get the same
// hostname for all three services unless they override.
//
// cluster.netbird.dnsZone (the mesh --dns-domain) is NOT defaulted here — it
// is operator-supplied via the prompt. netbird.networkRouter.dnsZone (an
// existing NetBird Mgmt DNS zone, a different field) IS defaulted, from the
// same base as stun/turn.
//
// Also derives netbird-operator defaults: networkRouter.replicas (1) and
// clusterProxy.clusterName (cluster.name).
//
// No-op when the netbird block is absent. The stun/turn/router-dnsZone
// derivations additionally require a non-empty cfg.DNS; replica and
// cluster-proxy-name defaults do not.
func hydrateNetBirdDefaults() {
	cfg := config.ParsedGeneralConfig.Cluster.NetBird
	if cfg == nil {
		return
	}

	if cfg.DNS != "" {
		base := strings.TrimPrefix(cfg.DNS, "netbird.")

		if cfg.StunDNS == "" {
			cfg.StunDNS = "stun." + base
		}

		if cfg.TurnDNS == "" {
			cfg.TurnDNS = "turn." + base
		}

		if cfg.TurnUser == "" {
			cfg.TurnUser = "netbird"
		}

		// NetworkRouter publishes records under an existing NetBird Mgmt
		// DNS zone; default it to the same base stun/turn derive from
		// (netbird.vpn.acme.com → vpn.acme.com). Operator-overridable.
		if cfg.NetworkRouter != nil && cfg.NetworkRouter.DNSZone == "" {
			cfg.NetworkRouter.DNSZone = base
		}
	}

	// Replica count and cluster-proxy name don't depend on the Mgmt DNS,
	// so they're derived even when cfg.DNS is empty (a cluster pointing at
	// a parent VPN's Mgmt).
	if cfg.NetworkRouter != nil && cfg.NetworkRouter.Replicas == 0 {
		cfg.NetworkRouter.Replicas = 1
	}
	if cfg.ClusterProxy != nil && cfg.ClusterProxy.ClusterName == "" {
		cfg.ClusterProxy.ClusterName = config.ParsedGeneralConfig.Cluster.Name
	}
}
