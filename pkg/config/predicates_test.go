// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package config_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
)

// TestHCloudNATGatewayNeeded pins the NAT-gateway decision to "does any
// HCloud node lack a public IP". It is deliberately independent of the
// control-plane replica count, and of the other two behaviours
// HCloudSingleNodePublic gates (network.type=public and skipping the
// control-plane LB), which both stay tied to the one-replica topology.
func TestHCloudNATGatewayNeeded(t *testing.T) {
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

	withHCloudNodeGroup := func(cfg *config.GeneralConfig) *config.GeneralConfig {
		cfg.Cloud.Hetzner.NodeGroups.HCloud = []config.HCloudAutoScalableNodeGroup{{}}
		return cfg
	}

	tests := []struct {
		name string
		cfg  *config.GeneralConfig
		want bool
	}{
		{
			// publicNetwork.enabled puts every control-plane node on its own
			// public IPv4, so nothing is left to masquerade for.
			name: "vpn, 3 control-plane replicas, no node-group",
			cfg:  generalConfig(constants.ClusterTypeVPN, constants.HetznerModeHCloud, 3),
			want: false,
		},
		{
			// network.type=public: the lone node is public already.
			name: "vpn, 1 replica, no node-group",
			cfg:  generalConfig(constants.ClusterTypeVPN, constants.HetznerModeHCloud, 1),
			want: false,
		},
		{
			// HCloud worker node-groups never get a public IP.
			name: "vpn, 3 replicas with an HCloud node-group",
			cfg: withHCloudNodeGroup(
				generalConfig(constants.ClusterTypeVPN, constants.HetznerModeHCloud, 3),
			),
			want: true,
		},
		{
			// Bare-metal workers reach the internet over their own public
			// IPs and never run /connect-nat-gateway.sh; validation rejects
			// vpn+hybrid today, but the predicate stays well-defined.
			name: "vpn, hybrid mode, no HCloud node-group",
			cfg:  generalConfig(constants.ClusterTypeVPN, constants.HetznerModeHybrid, 3),
			want: false,
		},
		{
			// Multi-replica workload cluster: private-only control plane.
			name: "workload, 3 replicas",
			cfg:  generalConfig(constants.ClusterTypeWorkload, constants.HetznerModeHCloud, 3),
			want: true,
		},
		{
			// The single-node public topology is not VPN-only.
			name: "workload, 1 replica, no node-group",
			cfg:  generalConfig(constants.ClusterTypeWorkload, constants.HetznerModeHCloud, 1),
			want: false,
		},
		{
			name: "pure bare-metal has no HCloud Network",
			cfg: func() *config.GeneralConfig {
				cfg := generalConfig(constants.ClusterTypeVPN, constants.HetznerModeBareMetal, 3)
				cfg.Cloud.Hetzner.ControlPlane.HCloud = nil
				return cfg
			}(),
			want: false,
		},
		{
			name: "non-Hetzner cloud",
			cfg:  &config.GeneralConfig{Cluster: config.ClusterConfig{Type: constants.ClusterTypeVPN}},
			want: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config.ParsedGeneralConfig = test.cfg
			assert.Equal(t, test.want, config.HCloudNATGatewayNeeded())
		})
	}
}

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
