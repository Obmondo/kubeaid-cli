// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package hetzner

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetupDisasterRecovery(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		wantErrMsg string
	}{
		{
			name:       "always returns not implemented error",
			wantErrMsg: "not implemented",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := &Hetzner{}
			err := h.SetupDisasterRecovery(context.Background())
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErrMsg)
		})
	}
}
