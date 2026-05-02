// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package prompt

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAzurePrompter_SummaryLines(t *testing.T) {
	tests := []struct {
		name string
		cfg  *PromptedConfig
		want []string
	}{
		{
			name: "all fields populated",
			cfg: &PromptedConfig{
				AzureLocation:     "westeurope",
				AzureCPVMSize:     "Standard_B2s",
				AzureCPDiskSizeGB: "128",
				AzureCPReplicas:   "1",
			},
			want: []string{
				"  Location:      westeurope",
				"  VM size:       Standard_B2s",
				"  Disk size:     128 GB",
				"  CP replicas:   1",
			},
		},
		{
			name: "disk size always carries GB suffix",
			cfg: &PromptedConfig{
				AzureCPDiskSizeGB: "",
			},
			want: []string{
				"  Location:      ",
				"  VM size:       ",
				"  Disk size:      GB",
				"  CP replicas:   ",
			},
		},
	}

	p := newAzureProvider()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, p.SummaryLines(tc.cfg))
		})
	}
}
