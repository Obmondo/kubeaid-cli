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
// The mesh DNS zone defaults to "<cluster.name>.local" for any cluster that
// carries a netbird block (vpn host or workload joiner), independent of DNS.
// The stun/turn/user derivation is VPN-host-only (needs the Mgmt DNS).
//
// No-op when the netbird block is absent.
func hydrateNetBirdDefaults() {
	cfg := config.ParsedGeneralConfig.Cluster.NetBird
	if cfg == nil {
		return
	}

	if cfg.DNSZone == "" {
		cfg.DNSZone = DefaultNetBirdDNSZone(config.ParsedGeneralConfig.Cluster.Name)
	}

	// stun/turn/user are derived from the Mgmt DNS — only the VPN host sets it.
	if cfg.DNS == "" {
		return
	}

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

// DefaultNetBirdDNSZone is the mesh DNS domain a cluster falls back to when
// cluster.netbird.dnsZone is empty: "<cluster-name>.local". Exported so the
// template layer can compute the same default for clusters with no
// cluster.netbird block at all (the apiserver SAN kubernetes.<zone> is added
// regardless of whether the operator declared a netbird block).
func DefaultNetBirdDNSZone(clusterName string) string {
	return clusterName + ".local"
}
