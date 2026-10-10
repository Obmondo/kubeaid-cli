// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package config_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
)

// TestHCloudControlPlanePublicNetwork pins the predicate to cluster.type
// rather than to the control-plane replica count: a VPN cluster runs Coturn,
// which needs a public address on the node (and a Primary IP before HCloud
// will attach a Floating IP), so scaling the control-plane from 1 to 3 must
// not take the public IPv4 away.
func TestHCloudControlPlanePublicNetwork(t *testing.T) {
	orig := config.ParsedGeneralConfig
	t.Cleanup(func() { config.ParsedGeneralConfig = orig })

	generalConfig := func(clusterType, mode string, replicas uint) *config.GeneralConfig {
		return &config.GeneralConfig{
			Cluster: config.ClusterConfig{Type: clusterType},
			Cloud: config.CloudConfig{
				Hetzner: &config.HetznerConfig{
					Mode: mode,
					ControlPlane: config.HetznerControlPlane{
						HCloud: &config.HCloudControlPlane{
							Replicas:    replicas,
							MachineType: "cpx41",
						},
					},
				},
			},
		}
	}

	tests := []struct {
		name string
		cfg  *config.GeneralConfig
		want bool
	}{
		{
			name: "vpn, 3 control-plane replicas",
			cfg:  generalConfig(constants.ClusterTypeVPN, constants.HetznerModeHCloud, 3),
			want: true,
		},
		{
			// Hybrid puts the control-plane in HCloud too, and
			// CoturnFloatingIPEnabled already covers it.
			name: "vpn, hybrid mode",
			cfg:  generalConfig(constants.ClusterTypeVPN, constants.HetznerModeHybrid, 3),
			want: true,
		},
		{
			// One replica and no HCloud node-group is the single-node public
			// topology: network.type=public already makes the node public.
			name: "vpn, 1 replica: single-node public path owns it",
			cfg:  generalConfig(constants.ClusterTypeVPN, constants.HetznerModeHCloud, 1),
			want: false,
		},
		{
			// An HCloud node-group needs the NAT gateway, so the cluster stays
			// on the private network and the control-plane needs the flag.
			name: "vpn, 1 replica with an HCloud node-group",
			cfg: func() *config.GeneralConfig {
				cfg := generalConfig(constants.ClusterTypeVPN, constants.HetznerModeHCloud, 1)
				cfg.Cloud.Hetzner.NodeGroups.HCloud = []config.HCloudAutoScalableNodeGroup{{}}
				return cfg
			}(),
			want: true,
		},
		{
			name: "workload cluster runs no Coturn",
			cfg:  generalConfig(constants.ClusterTypeWorkload, constants.HetznerModeHCloud, 3),
			want: false,
		},
		{
			name: "vpn with a bare-metal control-plane",
			cfg: func() *config.GeneralConfig {
				cfg := generalConfig(constants.ClusterTypeVPN, constants.HetznerModeBareMetal, 3)
				cfg.Cloud.Hetzner.ControlPlane.HCloud = nil
				return cfg
			}(),
			want: false,
		},
		{
			name: "vpn on a non-Hetzner cloud",
			cfg:  &config.GeneralConfig{Cluster: config.ClusterConfig{Type: constants.ClusterTypeVPN}},
			want: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config.ParsedGeneralConfig = test.cfg
			assert.Equal(t, test.want, config.HCloudControlPlanePublicNetwork())
		})
	}
}
