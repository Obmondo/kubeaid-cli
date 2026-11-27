// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package azure

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateCapiClusterValuesFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		updates any
		wantErr bool
		errMsg  string
	}{
		{
			name:    "empty NewImageOffer is a no-op",
			updates: AzureMachineTemplateUpdates{NewImageOffer: ""},
		},
		{
			name:    "wrong updates type string returns error",
			updates: "not-a-valid-type",
			wantErr: true,
			errMsg:  "wrong type of MachineTemplateUpdates object passed",
		},
		{
			name:    "wrong updates type int returns error",
			updates: 42,
			wantErr: true,
			errMsg:  "wrong type of MachineTemplateUpdates object passed",
		},
		{
			name:    "wrong updates type nil returns error",
			updates: nil,
			wantErr: true,
			errMsg:  "wrong type of MachineTemplateUpdates object passed",
		},
		{
			name:    "wrong updates type struct returns error",
			updates: struct{ Foo string }{"bar"},
			wantErr: true,
			errMsg:  "wrong type of MachineTemplateUpdates object passed",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			a := &Azure{}
			err := a.UpdateCapiClusterValuesFile(context.Background(), "/unused", tc.updates)
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errMsg)
				return
			}
			require.NoError(t, err)
		})
	}
}
