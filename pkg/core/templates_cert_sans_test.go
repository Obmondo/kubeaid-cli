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
		name    string
		netbird *config.NetBirdConfig
		hetzner *config.HetznerConfig
		want    []string
	}{
		{
			name:    "no netbird block, no extras: empty",
			netbird: nil,
			hetzner: &config.HetznerConfig{},
			want:    nil,
		},
		{
			name:    "no netbird block: only operator extraCertSANs",
			netbird: nil,
			hetzner: &config.HetznerConfig{
				ControlPlane: config.HetznerControlPlane{ExtraCertSANs: []string{"a.acme.com"}},
			},
			want: []string{"a.acme.com"},
		},
		{
			name:    "empty dnsZone (block present): only operator extraCertSANs",
			netbird: &config.NetBirdConfig{},
			hetzner: &config.HetznerConfig{
				ControlPlane: config.HetznerControlPlane{ExtraCertSANs: []string{"a.acme.com"}},
			},
			want: []string{"a.acme.com"},
		},
		{
			// dnsZone is retained for NetBird --dns-domain but must NOT produce a
			// kubernetes.<zone> cert SAN — the NetBird kube-apiserver proxy makes
			// that SAN unnecessary.
			name:    "dnsZone set, no extraCertSANs: result is empty (no mesh SAN)",
			netbird: &config.NetBirdConfig{DNSZone: "mesh.acme.com"},
			hetzner: &config.HetznerConfig{},
			want:    nil,
		},
		{
			// dnsZone must NOT appear in the SAN list; operator extraCertSANs pass through.
			name:    "dnsZone + operator extraCertSANs: only extraCertSANs returned",
			netbird: &config.NetBirdConfig{DNSZone: "mesh.acme.com"},
			hetzner: &config.HetznerConfig{
				ControlPlane: config.HetznerControlPlane{ExtraCertSANs: []string{"a.acme.com", "b.acme.com"}},
			},
			want: []string{"a.acme.com", "b.acme.com"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			withFreshGeneralConfig(t, func() {
				config.ParsedGeneralConfig.Cluster.NetBird = tc.netbird

				assert.Equal(t, tc.want, hetznerControlPlaneCertSANs(tc.hetzner))
			})
		})
	}
}
