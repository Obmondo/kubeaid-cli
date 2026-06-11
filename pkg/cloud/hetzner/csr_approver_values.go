// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package hetzner

import (
	"context"
	"fmt"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
)

// GetHetznerBareMetalHostPublicIPs returns a map of HetznerBareMetalHost
// ServerID to the Robot main IP for that server. Used by the kubelet-csr-
// approver values template to widen providerIpPrefixes with one /32 per
// node (kubelet's serving-cert SAN list always contains every local
// interface IP — both vSwitch and public — so the approver's allow-list
// has to cover both or every CSR is denied).
//
// Returns an empty map on non-bare-metal Hetzner setups; the matching
// values template guards on map presence and skips iteration. Robot API
// calls are sequential (small N, Robot's per-account rate limit is
// unforgiving).
//
// Follow-up removal: once
// https://github.com/syself/cluster-api-provider-hetzner/issues/2095
// lands and the CAPH dep is bumped to a version that includes the fix,
// the kubelet-csr-approver chart + this helper + its callers can all be
// deleted (CAPH's native CSR validator is structurally tighter than
// postfinance's IP-prefix allow-list).
func (h *Hetzner) GetHetznerBareMetalHostPublicIPs(ctx context.Context) (map[string]string, error) {
	if !config.UsingHetznerBareMetal() {
		return map[string]string{}, nil
	}

	hetznerConfig := config.ParsedGeneralConfig.Cloud.Hetzner
	if hetznerConfig.BareMetal == nil || hetznerConfig.BareMetal.VSwitch == nil {
		return map[string]string{}, nil
	}

	var hosts []*config.HetznerBareMetalHost
	if config.ControlPlaneInHetznerBareMetal() {
		hosts = append(hosts, hetznerConfig.ControlPlane.BareMetal.BareMetalHosts...)
	}
	for _, nodeGroup := range hetznerConfig.NodeGroups.BareMetal {
		hosts = append(hosts, nodeGroup.BareMetalHosts...)
	}

	result := make(map[string]string, len(hosts))
	for _, host := range hosts {
		publicIP, err := h.getHetznerBareMetalServerIP(host.ServerID)
		if err != nil {
			return nil, fmt.Errorf("fetching Robot main IP for server %s: %w", host.ServerID, err)
		}
		result[host.ServerID] = publicIP
	}
	return result, nil
}
