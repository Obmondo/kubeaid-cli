// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package templates

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	templateUtils "github.com/Obmondo/kubeaid-cli/pkg/utils/templates"
)

// renderEmbeddedPath renders the embedded template at path — as it is named in
// KubeaidConfigFileTemplates, such as "templates/argocd-apps/values-capi-cluster.yaml.tmpl" —
// against values.
func renderEmbeddedPath(t *testing.T, path string, values any) []byte {
	t.Helper()

	rendered, err := templateUtils.ParseAndExecuteTemplate(&KubeaidConfigFileTemplates, path, values)
	require.NoError(t, err)
	return rendered
}

// newTestConfig returns a config.Config over a general config whose Hetzner block is hetzner.
func newTestConfig(hetzner *config.HetznerConfig) *config.Config {
	return &config.Config{
		General: &config.GeneralConfig{Cloud: config.CloudConfig{Hetzner: hetzner}},
		Secrets: &config.SecretsConfig{},
	}
}

func TestHCloudControlPlaneEndpointSet(t *testing.T) {
	tests := []struct {
		name    string
		hetzner *config.HetznerConfig
		lbIP    string // populated to Config.Provisioned.ControlPlaneLBPrivateIP
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
			cfg := newTestConfig(tc.hetzner)
			cfg.Provisioned.ControlPlaneLBPrivateIP = tc.lbIP

			assert.Equal(t, tc.want, hcloudControlPlaneEndpointSet(cfg))
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
