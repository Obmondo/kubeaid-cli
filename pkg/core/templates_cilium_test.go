// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package core

import (
	"bytes"
	"testing"
	"text/template"

	"github.com/go-sprout/sprout"
	"github.com/go-sprout/sprout/registry/encoding"
	sproutstrings "github.com/go-sprout/sprout/registry/strings"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
)

// renderCiliumValuesTemplate executes values-cilium.yaml.tmpl against the
// given TemplateValues and returns the rendered string. Mirrors the sprout
// FuncMap used in production (encoding + strings registries).
func renderCiliumValuesTemplate(values TemplateValues) (string, error) {
	const tmplPath = "templates/argocd-apps/values-cilium.yaml.tmpl"

	raw, err := KubeaidConfigFileTemplates.ReadFile(tmplPath)
	if err != nil {
		return "", err
	}

	sproutFuncs := sprout.New(sprout.WithRegistries(
		encoding.NewRegistry(),
		sproutstrings.NewRegistry(),
	)).Build()

	t, err := template.New(tmplPath).Funcs(sproutFuncs).Parse(string(raw))
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, values); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func boolPtr(v bool) *bool { return &v }

func TestHetznerBareMetalFirewallEnabled(t *testing.T) {
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
			name:    "hetzner config without BareMetal block: false",
			hetzner: &config.HetznerConfig{},
			want:    false,
		},
		{
			name: "BareMetal block with Firewall.Enabled nil (default true): true",
			hetzner: &config.HetznerConfig{
				BareMetal: &config.HetznerBareMetalConfig{},
			},
			want: true,
		},
		{
			name: "BareMetal block with Firewall.Enabled explicitly true: true",
			hetzner: &config.HetznerConfig{
				BareMetal: &config.HetznerBareMetalConfig{
					Firewall: config.FirewallConfig{Enabled: boolPtr(true)},
				},
			},
			want: true,
		},
		{
			name: "BareMetal block with Firewall.Enabled explicitly false: false",
			hetzner: &config.HetznerConfig{
				BareMetal: &config.HetznerBareMetalConfig{
					Firewall: config.FirewallConfig{Enabled: boolPtr(false)},
				},
			},
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			withFreshGeneralConfig(t, func() {
				config.ParsedGeneralConfig.Cloud.Hetzner = tc.hetzner
				assert.Equal(t, tc.want, hetznerBareMetalFirewallEnabled())
			})
		})
	}
}

// TestCiliumValuesTemplateHostNetworkPolicy exercises the
// hostNetworkPolicy block in values-cilium.yaml.tmpl against
// known TemplateValues inputs.
func TestCiliumValuesTemplateHostNetworkPolicy(t *testing.T) {
	tests := []struct {
		name            string
		tmplValues      TemplateValues
		wantContains    []string
		wantNotContains []string
		wantErr         bool
	}{
		{
			name: "firewall enabled with allowSshFrom set: renders allowSshFrom entries",
			tmplValues: TemplateValues{
				HetznerBareMetalFirewallEnabled: true,
				HetznerConfig: &config.HetznerConfig{
					BareMetal: &config.HetznerBareMetalConfig{
						Firewall: config.FirewallConfig{
							AllowSSHFrom: []string{"192.0.2.1", "198.51.100.0/24"},
						},
					},
				},
			},
			wantContains: []string{
				"hostNetworkPolicy:",
				"enabled: false",
				"allowSshFrom:",
				"- 192.0.2.1",
				"- 198.51.100.0/24",
				// hostFirewall must be enabled alongside the policy.
				"hostFirewall:",
			},
		},
		{
			name: "firewall enabled with empty allowSshFrom: no allowSshFrom key",
			tmplValues: TemplateValues{
				HetznerBareMetalFirewallEnabled: true,
				HetznerConfig: &config.HetznerConfig{
					BareMetal: &config.HetznerBareMetalConfig{},
				},
			},
			wantContains: []string{
				"hostNetworkPolicy:",
				"enabled: false",
				"hostFirewall:",
			},
			wantNotContains: []string{"allowSshFrom:"},
		},
		{
			name: "firewall disabled: no hostNetworkPolicy or hostFirewall block",
			tmplValues: TemplateValues{
				HetznerBareMetalFirewallEnabled: false,
			},
			wantNotContains: []string{"hostNetworkPolicy:", "hostFirewall:"},
		},
		{
			name: "non-bare-metal (HetznerConfig nil, flag false): no hostNetworkPolicy block",
			tmplValues: TemplateValues{
				HetznerBareMetalFirewallEnabled: false,
				HetznerConfig:                  nil,
			},
			wantNotContains: []string{"hostNetworkPolicy:"},
		},
		{
			// Defensive: flag set to true but HetznerConfig is nil. This cannot
			// happen via getTemplateValues (hetznerBareMetalFirewallEnabled
			// returns false when HetznerConfig is nil), but the template guard
			// {{- if and .HetznerBareMetalFirewallEnabled .HetznerConfig .HetznerConfig.BareMetal }}
			// must short-circuit safely before accessing BareMetal.
			name: "flag true but HetznerConfig nil: no hostNetworkPolicy block (nil-safe guard)",
			tmplValues: TemplateValues{
				HetznerBareMetalFirewallEnabled: true,
				HetznerConfig:                  nil,
			},
			wantNotContains: []string{"hostNetworkPolicy:"},
		},
		{
			// Both the cilium block (gated on ControlPlaneEndpoint) and the
			// hostNetworkPolicy + hostFirewall blocks (gated on HetznerBareMetalFirewallEnabled)
			// must coexist in the same rendered output without YAML errors.
			name: "both cilium kube-proxy and hostFirewall blocks render together",
			tmplValues: TemplateValues{
				ControlPlaneEndpoint:            "cp.acme.com",
				HetznerBareMetalFirewallEnabled: true,
				HetznerConfig: &config.HetznerConfig{
					BareMetal: &config.HetznerBareMetalConfig{},
				},
			},
			wantContains: []string{
				"cilium:",
				"k8sServiceHost: cp.acme.com",
				"extraArgs:",
				"hostFirewall:",
				"hostNetworkPolicy:",
				"enabled: false",
			},
			wantNotContains: []string{"allowSshFrom:"},
		},
		{
			// Paired-artifact check: when the firewall flag is true but no
			// ControlPlaneEndpoint is set (early bootstrap), hostFirewall must
			// still be rendered inside cilium: so the CCNP is actually enforced.
			name: "firewall enabled without ControlPlaneEndpoint: hostFirewall still rendered",
			tmplValues: TemplateValues{
				ControlPlaneEndpoint:            "",
				HetznerBareMetalFirewallEnabled: true,
				HetznerConfig: &config.HetznerConfig{
					BareMetal: &config.HetznerBareMetalConfig{},
				},
			},
			wantContains: []string{
				"cilium:",
				"hostFirewall:",
				"hostNetworkPolicy:",
			},
			// k8sServiceHost must not appear — no endpoint was set.
			wantNotContains: []string{"k8sServiceHost:"},
		},
		{
			name: "single allowSshFrom entry: renders exactly one CIDR line",
			tmplValues: TemplateValues{
				HetznerBareMetalFirewallEnabled: true,
				HetznerConfig: &config.HetznerConfig{
					BareMetal: &config.HetznerBareMetalConfig{
						Firewall: config.FirewallConfig{
							AllowSSHFrom: []string{"192.0.2.1"},
						},
					},
				},
			},
			wantContains: []string{
				"allowSshFrom:",
				"- 192.0.2.1",
			},
			// A second entry must not appear.
			wantNotContains: []string{"198.51.100.0"},
		},
		{
			name: "HetznerBareMetalHostPublicIPs set: renders apiserverSourceCIDRs",
			tmplValues: TemplateValues{
				HetznerBareMetalFirewallEnabled: true,
				HetznerConfig: &config.HetznerConfig{
					BareMetal: &config.HetznerBareMetalConfig{},
				},
				HetznerBareMetalHostPublicIPs: map[string]string{
					"1234": "192.0.2.1",
					"5678": "192.0.2.2",
				},
			},
			wantContains: []string{
				"apiserverSourceCIDRs:",
				"192.0.2.1/32",
				"192.0.2.2/32",
				"enabled: false",
			},
		},
		{
			name: "HetznerBareMetalHostPublicIPs empty: no apiserverSourceCIDRs key",
			tmplValues: TemplateValues{
				HetznerBareMetalFirewallEnabled: true,
				HetznerConfig: &config.HetznerConfig{
					BareMetal: &config.HetznerBareMetalConfig{},
				},
				HetznerBareMetalHostPublicIPs: nil,
			},
			wantNotContains: []string{"apiserverSourceCIDRs:"},
			wantContains:    []string{"enabled: false"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rendered, err := renderCiliumValuesTemplate(tc.tmplValues)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			for _, want := range tc.wantContains {
				assert.Contains(t, rendered, want, "expected %q in rendered output", want)
			}
			for _, notWant := range tc.wantNotContains {
				assert.NotContains(t, rendered, notWant, "expected %q absent from rendered output", notWant)
			}
		})
	}
}
