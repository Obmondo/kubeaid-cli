// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package azure

import (
	"context"
	"fmt"
	"testing"

	"github.com/Azure/azure-sdk-for-go/profile/p20200901/resourcemanager/compute/armcompute"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-cli/pkg/cloud"
	"github.com/Obmondo/kubeaid-cli/pkg/config"
)

// Mutates config.ParsedGeneralConfig — sequential only.
func TestGetVMSpecs(t *testing.T) {
	ptr := func(s string) *string { return &s }
	i32 := func(v int32) *int32 { return &v }

	tests := []struct {
		name        string
		vmType      string
		listVMSizes func(ctx context.Context, location string) ([]*armcompute.VirtualMachineSize, error)
		want        *cloud.VMSpec
		wantErr     bool
		errContains string
	}{
		{
			name:   "finds matching VM type",
			vmType: "Standard_D2s_v3",
			listVMSizes: func(_ context.Context, _ string) ([]*armcompute.VirtualMachineSize, error) {
				return []*armcompute.VirtualMachineSize{
					{Name: ptr("Standard_D2s_v3"), NumberOfCores: i32(2), MemoryInMB: i32(8192)},
				}, nil
			},
			want: &cloud.VMSpec{CPU: 2, Memory: 8},
		},
		{
			name:   "finds correct VM among multiple",
			vmType: "Standard_D4s_v3",
			listVMSizes: func(_ context.Context, _ string) ([]*armcompute.VirtualMachineSize, error) {
				return []*armcompute.VirtualMachineSize{
					{Name: ptr("Standard_D2s_v3"), NumberOfCores: i32(2), MemoryInMB: i32(8192)},
					{Name: ptr("Standard_D4s_v3"), NumberOfCores: i32(4), MemoryInMB: i32(16384)},
					{Name: ptr("Standard_D8s_v3"), NumberOfCores: i32(8), MemoryInMB: i32(32768)},
				}, nil
			},
			want: &cloud.VMSpec{CPU: 4, Memory: 16},
		},
		{
			name:   "listVMSizes returns error",
			vmType: "Standard_D2s_v3",
			listVMSizes: func(_ context.Context, _ string) ([]*armcompute.VirtualMachineSize, error) {
				return nil, fmt.Errorf("API unavailable")
			},
			wantErr:     true,
			errContains: "fetching VM sizes list",
		},
		{
			name:   "VM type not found",
			vmType: "Standard_NonExistent",
			listVMSizes: func(_ context.Context, _ string) ([]*armcompute.VirtualMachineSize, error) {
				return []*armcompute.VirtualMachineSize{
					{Name: ptr("Standard_D2s_v3"), NumberOfCores: i32(2), MemoryInMB: i32(8192)},
				}, nil
			},
			wantErr:     true,
			errContains: `VM type "Standard_NonExistent" not found`,
		},
		{
			name:   "empty VM list returns not-found error",
			vmType: "Standard_D2s_v3",
			listVMSizes: func(_ context.Context, _ string) ([]*armcompute.VirtualMachineSize, error) {
				return []*armcompute.VirtualMachineSize{}, nil
			},
			wantErr:     true,
			errContains: "not found",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			saved := config.ParsedGeneralConfig
			t.Cleanup(func() { config.ParsedGeneralConfig = saved })

			config.ParsedGeneralConfig = &config.GeneralConfig{
				Cloud: config.CloudConfig{
					Azure: &config.AzureConfig{
						Location: "eastus",
					},
				},
			}

			a := &Azure{
				listVMSizes: tc.listVMSizes,
			}

			got, err := a.GetVMSpecs(context.Background(), tc.vmType)
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errContains)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
