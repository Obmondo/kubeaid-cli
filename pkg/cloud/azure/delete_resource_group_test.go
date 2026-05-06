// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package azure

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeleteResourceGroup(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                  string
		resourceGroupName     string
		deleteResourceGroupFn func(ctx context.Context, name string) error
		wantErr               bool
		errContains           string
	}{
		{
			name:              "successful deletion",
			resourceGroupName: "my-resource-group",
			deleteResourceGroupFn: func(_ context.Context, name string) error {
				return nil
			},
		},
		{
			name:              "passes correct resource group name",
			resourceGroupName: "test-rg-42",
			deleteResourceGroupFn: func(_ context.Context, name string) error {
				if name != "test-rg-42" {
					return fmt.Errorf("unexpected name: %s", name)
				}
				return nil
			},
		},
		{
			name:              "deletion returns error",
			resourceGroupName: "failing-rg",
			deleteResourceGroupFn: func(_ context.Context, _ string) error {
				return fmt.Errorf("permission denied")
			},
			wantErr:     true,
			errContains: "permission denied",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			a := &Azure{
				resourceGroupName:     tc.resourceGroupName,
				deleteResourceGroupFn: tc.deleteResourceGroupFn,
			}

			err := a.DeleteResourceGroup(context.Background())
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errContains)
				return
			}
			require.NoError(t, err)
		})
	}
}
