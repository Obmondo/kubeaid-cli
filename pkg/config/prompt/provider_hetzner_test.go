// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package prompt

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHetznerPrompter_SummaryLines(t *testing.T) {
	tests := []struct {
		name string
		cfg  *PromptedConfig
		want []string
	}{
		{
			name: "hcloud mode shows zone, machine, LB region, replicas, then mode",
			cfg: &PromptedConfig{
				HetznerMode:          "hcloud",
				HetznerHCloudZone:    "eu-central",
				HetznerCPMachineType: "cax21",
				HetznerLBRegion:      "hel1",
				HetznerCPReplicas:    "3",
			},
			want: []string{
				"  Zone:          eu-central",
				"  Machine type:  cax21",
				"  LB region:     hel1",
				"  CP replicas:   3",
				"  Mode:          hcloud",
			},
		},
		{
			name: "bare-metal mode shows mode only",
			cfg: &PromptedConfig{
				HetznerMode: "bare-metal",
			},
			want: []string{
				"  Mode:          bare-metal",
			},
		},
		{
			name: "hybrid with hcloud zone shows the full block",
			cfg: &PromptedConfig{
				HetznerMode:          "hybrid",
				HetznerHCloudZone:    "eu-central",
				HetznerCPMachineType: "cax21",
				HetznerLBRegion:      "hel1",
				HetznerCPReplicas:    "1",
			},
			want: []string{
				"  Zone:          eu-central",
				"  Machine type:  cax21",
				"  LB region:     hel1",
				"  CP replicas:   1",
				"  Mode:          hybrid",
			},
		},
		{
			name: "hcloud mode but empty zone falls back to mode-only",
			cfg: &PromptedConfig{
				HetznerMode:       "hcloud",
				HetznerHCloudZone: "",
			},
			want: []string{
				"  Mode:          hcloud",
			},
		},
	}

	p := newHetznerProvider()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, p.SummaryLines(tc.cfg))
		})
	}
}
