// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package core

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
)

// withFreshGeneralConfig swaps ParsedGeneralConfig for the duration of fn
// so the test never leaks the package-level config state to other tests.
func withFreshGeneralConfig(t *testing.T, fn func()) {
	t.Helper()

	orig := config.ParsedGeneralConfig
	config.ParsedGeneralConfig = &config.GeneralConfig{}

	t.Cleanup(func() { config.ParsedGeneralConfig = orig })

	fn()
}

func TestManagedKeycloakEnabled(t *testing.T) {
	tests := []struct {
		name        string
		clusterType string
		keycloak    *config.KeycloakConfig
		want        bool
	}{
		{
			name:        "workload cluster without keycloak block: false",
			clusterType: constants.ClusterTypeWorkload,
			keycloak:    nil,
			want:        false,
		},
		{
			name:        "vpn cluster without keycloak block: false (nil-safe)",
			clusterType: constants.ClusterTypeVPN,
			keycloak:    nil,
			want:        false,
		},
		{
			name:        "vpn cluster with mode=external: false",
			clusterType: constants.ClusterTypeVPN,
			keycloak:    &config.KeycloakConfig{Mode: "external", DNS: "keycloak.acme.com"},
			want:        false,
		},
		{
			name:        "vpn cluster with mode=managed: true",
			clusterType: constants.ClusterTypeVPN,
			keycloak:    &config.KeycloakConfig{Mode: "managed", DNS: "keycloak.acme.com"},
			want:        true,
		},
		{
			// Schema validation prevents this combination at parse time, but
			// the gate itself must still be nil-safe and return false rather
			// than render a broken config.
			name:        "workload cluster with managed keycloak: false (defensive)",
			clusterType: constants.ClusterTypeWorkload,
			keycloak:    &config.KeycloakConfig{Mode: "managed", DNS: "keycloak.acme.com"},
			want:        false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			withFreshGeneralConfig(t, func() {
				config.ParsedGeneralConfig.Cluster.Type = tc.clusterType
				config.ParsedGeneralConfig.Cluster.Keycloak = tc.keycloak

				assert.Equal(t, tc.want, managedKeycloakEnabled())
			})
		})
	}
}

func TestHCloudControlPlaneEndpointSet(t *testing.T) {
	tests := []struct {
		name    string
		hetzner *config.HetznerConfig
		want    bool
	}{
		{
			name:    "nil hetzner config: false",
			hetzner: nil,
			want:    false,
		},
		{
			name:    "hetzner config without HCloud control-plane: false",
			hetzner: &config.HetznerConfig{},
			want:    false,
		},
		{
			name: "HCloud control-plane with empty endpoint: false",
			hetzner: &config.HetznerConfig{
				ControlPlane: config.HetznerControlPlane{
					HCloud: &config.HCloudControlPlane{},
				},
			},
			want: false,
		},
		{
			name: "HCloud control-plane with endpoint set: true",
			hetzner: &config.HetznerConfig{
				ControlPlane: config.HetznerControlPlane{
					HCloud: &config.HCloudControlPlane{
						LoadBalancer: config.HCloudControlPlaneLoadBalancer{
							Endpoint: "api.acme.com",
						},
					},
				},
			},
			want: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			withFreshGeneralConfig(t, func() {
				config.ParsedGeneralConfig.Cloud.Hetzner = tc.hetzner
				assert.Equal(t, tc.want, hcloudControlPlaneEndpointSet())
			})
		})
	}
}
