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
			name: "bare-metal mode shows server IDs and endpoint, omits hcloud fields",
			cfg: &PromptedConfig{
				HetznerMode: "bare-metal",
				// Defensive: any stale hcloud defaults left in cfg must NOT
				// leak into the summary — the gate is on HetznerMode, not
				// on HetznerHCloudZone being empty.
				HetznerHCloudZone:    "eu-central",
				HetznerCPMachineType: "cax21",
				HetznerLBRegion:      "hel1",
				HetznerCPReplicas:    "3",

				HetznerBMCPServerIDs:          []string{"1234567", "1234568", "1234569"},
				HetznerBMNodeGroupName:        "kbm-workers",
				HetznerBMNodeGroupServerIDs:   []string{"1234570"},
				HetznerBMEndpointHost:         "1.2.3.4",
				HetznerBMEndpointIsFailoverIP: true,
				HetznerBMServerPublicIPs: map[string]string{
					"1234567": "5.5.5.1",
					"1234568": "5.5.5.2",
					"1234569": "5.5.5.3",
					"1234570": "5.5.5.4",
				},
			},
			want: []string{
				"  CP replicas:   3",
				"  CP servers:    1234567 (5.5.5.1), 1234568 (5.5.5.2), 1234569 (5.5.5.3)",
				"  Worker group:  kbm-workers",
				"  Worker hosts:  1234570 (5.5.5.4)",
				"  API endpoint:  1.2.3.4 (Failover IP)",
				"  Mode:          bare-metal",
			},
		},
		{
			name: "bare-metal mode without Robot validation (no public IPs yet)",
			cfg: &PromptedConfig{
				HetznerMode:                 "bare-metal",
				HetznerCPReplicas:           "1",
				HetznerBMCPServerIDs:        []string{"1234567"},
				HetznerBMNodeGroupName:      "kbm-workers",
				HetznerBMNodeGroupServerIDs: []string{"1234570"},
				HetznerBMEndpointHost:       "1.2.3.4",
			},
			want: []string{
				"  CP replicas:   1",
				"  CP servers:    1234567",
				"  Worker group:  kbm-workers",
				"  Worker hosts:  1234570",
				"  API endpoint:  1.2.3.4",
				"  Mode:          bare-metal",
			},
		},
		{
			name: "hybrid mode shows the full hcloud block (CP lives in HCloud)",
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
	}

	p := newHetznerProvider()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, p.SummaryLines(tc.cfg))
		})
	}
}
