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
// No-op when the netbird block is absent or DNS is empty.
func hydrateNetBirdDefaults() {
	cfg := config.ParsedGeneralConfig.Cluster.NetBird
	if cfg == nil || cfg.DNS == "" {
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
