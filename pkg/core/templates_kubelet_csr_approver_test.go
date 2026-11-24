// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
)

func TestKubeletCSRApproverValuesTemplate(t *testing.T) {
	const tmplPath = "templates/argocd-apps/values-kubelet-csr-approver.yaml.tmpl"

	tests := []struct {
		name            string
		tmplValues      TemplateValues
		wantPrefix      string
		wantContains    []string
		wantNotContains []string
	}{
		{
			name: "HetznerBareMetalHostPublicIPs set: no leading blank line, vSwitch CIDR plus one /32 per host",
			tmplValues: TemplateValues{
				HetznerConfig: &config.HetznerConfig{
					BareMetal: &config.HetznerBareMetalConfig{
						VSwitch: &config.VSwitchConfig{SubnetCIDRBlock: "10.0.1.0/24"},
					},
				},
				HetznerBareMetalHostPublicIPs: map[string]string{
					"1234": "192.0.2.1",
					"5678": "192.0.2.2",
				},
			},
			wantPrefix: "kubelet-csr-approver:",
			wantContains: []string{
				`providerIpPrefixes: "10.0.1.0/24,192.0.2.1/32,192.0.2.2/32"`,
			},
		},
		{
			name: "HetznerBareMetalHostPublicIPs nil: no kubelet-csr-approver block",
			tmplValues: TemplateValues{
				HetznerBareMetalHostPublicIPs: nil,
			},
			wantNotContains: []string{"kubelet-csr-approver:"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rendered := renderEmbeddedTemplate(t, tmplPath, tc.tmplValues)

			assert.True(t, strings.HasPrefix(rendered, tc.wantPrefix), "expected rendered output to start with %q, got %q", tc.wantPrefix, rendered)
			for _, want := range tc.wantContains {
				assert.Contains(t, rendered, want, "expected %q in rendered output", want)
			}
			for _, notWant := range tc.wantNotContains {
				assert.NotContains(t, rendered, notWant, "expected %q absent from rendered output", notWant)
			}
		})
	}
}
