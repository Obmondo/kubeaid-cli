// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package core

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/globals"
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

// withFreshGlobals snapshots and clears the package-level globals for
// the duration of fn — used by tests that exercise predicates depending
// on globals.ControlPlaneLB* state.
func withFreshGlobals(t *testing.T, fn func()) {
	t.Helper()

	origPrivIP := globals.ControlPlaneLBPrivateIP
	origPubIP := globals.ControlPlaneLBBootstrapPublicIP
	origHostname := globals.ControlPlaneHostname

	globals.ControlPlaneLBPrivateIP = ""
	globals.ControlPlaneLBBootstrapPublicIP = ""
	globals.ControlPlaneHostname = ""

	t.Cleanup(func() {
		globals.ControlPlaneLBPrivateIP = origPrivIP
		globals.ControlPlaneLBBootstrapPublicIP = origPubIP
		globals.ControlPlaneHostname = origHostname
	})

	fn()
}

func TestHCloudControlPlaneEndpointSet(t *testing.T) {
	tests := []struct {
		name    string
		hetzner *config.HetznerConfig
		lbIP    string // populated to globals.ControlPlaneLBPrivateIP
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
			name: "endpoint set but LB not pre-provisioned: false (would render empty hosts block)",
			hetzner: &config.HetznerConfig{
				ControlPlane: config.HetznerControlPlane{
					HCloud: &config.HCloudControlPlane{
						LoadBalancer: config.HCloudControlPlaneLoadBalancer{
							Endpoint: "api.acme.com",
						},
					},
				},
			},
			lbIP: "",
			want: false,
		},
		{
			name: "HCloud control-plane with endpoint set + LB pre-provisioned: true",
			hetzner: &config.HetznerConfig{
				ControlPlane: config.HetznerControlPlane{
					HCloud: &config.HCloudControlPlane{
						LoadBalancer: config.HCloudControlPlaneLoadBalancer{
							Endpoint: "api.acme.com",
						},
					},
				},
			},
			lbIP: "10.0.0.5",
			want: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			withFreshGeneralConfig(t, func() {
				withFreshGlobals(t, func() {
					config.ParsedGeneralConfig.Cloud.Hetzner = tc.hetzner
					globals.ControlPlaneLBPrivateIP = tc.lbIP
					assert.Equal(t, tc.want, hcloudControlPlaneEndpointSet())
				})
			})
		})
	}
}

func TestStoragectlVersion(t *testing.T) {
	cases := []struct {
		name             string
		operatorOverride string
		cliVersion       string
		want             string
	}{
		{
			name:       "no override, dev build yields empty (chart falls back to latest)",
			cliVersion: "dev",
			want:       "",
		},
		{
			name:       "no override, empty CLI version (unset ldflags) yields empty",
			cliVersion: "",
			want:       "",
		},
		{
			name:       "no override, release CLI version passes through verbatim",
			cliVersion: "v1.2.3",
			want:       "v1.2.3",
		},
		{
			name:       "no override, pre-release CLI tag passes through verbatim",
			cliVersion: "v1.2.3-rc.1",
			want:       "v1.2.3-rc.1",
		},
		{
			name:             "operator override wins over a release CLI version",
			operatorOverride: "v9.9.9",
			cliVersion:       "v1.2.3",
			want:             "v9.9.9",
		},
		{
			name:             "operator override unblocks dev builds (no release tagged yet)",
			operatorOverride: "v0.0.0-pre-release",
			cliVersion:       "dev",
			want:             "v0.0.0-pre-release",
		},
		{
			name:             "operator override unblocks empty CLI version too",
			operatorOverride: "v0.0.0-pre-release",
			cliVersion:       "",
			want:             "v0.0.0-pre-release",
		},
		{
			name:             "empty override falls back to CLI version (treats omitted-block and explicit-empty identically)",
			operatorOverride: "",
			cliVersion:       "v1.2.3",
			want:             "v1.2.3",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, storagectlVersion(tc.operatorOverride, tc.cliVersion))
		})
	}
}
