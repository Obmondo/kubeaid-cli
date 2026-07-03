// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package parser

import (
	"strings"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
)

// hydrateNetBirdDefaults derives stun/turn DNS names ("netbird.<base>" →
// "stun.<base>" / "turn.<base>"; a DNS without the "netbird." prefix is used
// as the base itself) and the static turn username, when unset. dnsZone stays
// operator-supplied. No-op when the netbird block is absent or DNS is empty.
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
}
