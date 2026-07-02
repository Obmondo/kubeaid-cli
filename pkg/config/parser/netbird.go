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
// is operator-supplied via the prompt.
//
// Also derives the netbird-operator clusterProxy.clusterName default
// (cluster.name). The network router's DNS zone is deliberately NOT derived —
// it's created by the operator in the NetBird dashboard, because deriving it
// from the cluster DNS tripped NetBird's domain-mismatch check.
//
// No-op when the netbird block is absent. The stun/turn derivations
// additionally require a non-empty cfg.DNS; the cluster-proxy-name default
// does not.
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
	}

	// ClusterProxy registers under a per-cluster label; default it to the
	// cluster name (netbird kubernetes write-kubeconfig <clusterName>).
	// Independent of the Mgmt DNS, so derived even when cfg.DNS is empty.
	if cfg.ClusterProxy != nil && cfg.ClusterProxy.ClusterName == "" {
		cfg.ClusterProxy.ClusterName = config.ParsedGeneralConfig.Cluster.Name
	}
}
