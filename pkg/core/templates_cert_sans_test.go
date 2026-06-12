// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package core

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
)

func TestHetznerControlPlaneCertSANs(t *testing.T) {
	tests := []struct {
		name        string
		clusterName string
		netbird     *config.NetBirdConfig
		hetzner     *config.HetznerConfig
		want        []string
	}{
		{
			name:        "no netbird block: kubernetes.<name>.local default",
			clusterName: "kbm-obmondo-com",
			netbird:     nil,
			hetzner:     &config.HetznerConfig{},
			want:        []string{"kubernetes.kbm-obmondo-com.local"},
		},
		{
			name:        "netbird dnsZone used verbatim",
			clusterName: "staging",
			netbird:     &config.NetBirdConfig{DNSZone: "staging.mesh.internal"},
			hetzner:     &config.HetznerConfig{},
			want:        []string{"kubernetes.staging.mesh.internal"},
		},
		{
			name:        "controlPlane.extraCertSANs appended (any mode)",
			clusterName: "kbm",
			netbird:     nil,
			hetzner: &config.HetznerConfig{
				ControlPlane: config.HetznerControlPlane{
					ExtraCertSANs: []string{"a.acme.com", "b.acme.com"},
				},
			},
			want: []string{"kubernetes.kbm.local", "a.acme.com", "b.acme.com"},
		},
		{
			name:        "hcloud cluster: controlPlane.extraCertSANs applied (same field, all modes)",
			clusterName: "vpn",
			netbird:     &config.NetBirdConfig{DNSZone: "vpn.local"},
			hetzner: &config.HetznerConfig{
				ControlPlane: config.HetznerControlPlane{
					ExtraCertSANs: []string{"a.acme.com"},
					HCloud:        &config.HCloudControlPlane{},
				},
			},
			want: []string{"kubernetes.vpn.local", "a.acme.com"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			withFreshGeneralConfig(t, func() {
				config.ParsedGeneralConfig.Cluster.Name = tc.clusterName
				config.ParsedGeneralConfig.Cluster.NetBird = tc.netbird

				assert.Equal(t, tc.want, hetznerControlPlaneCertSANs(tc.hetzner))
			})
		})
	}
}
