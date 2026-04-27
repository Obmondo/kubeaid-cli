// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package utils

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWrapError(t *testing.T) {
	sentinel := errors.New("connection refused")

	tests := []struct {
		name        string
		contextual  string
		inner       error
		wantMessage string
		wantTarget  error
	}{
		{
			name:        "happy path joins context and inner",
			contextual:  "failed to reach API",
			inner:       sentinel,
			wantMessage: "failed to reach API : connection refused",
			wantTarget:  sentinel,
		},
		{
			name:        "empty contextual message still produces a valid wrap",
			contextual:  "",
			inner:       sentinel,
			wantMessage: " : connection refused",
			wantTarget:  sentinel,
		},
		{
			name:        "double-wrap preserves the chain",
			contextual:  "outer",
			inner:       fmt.Errorf("middle : %w", sentinel),
			wantMessage: "outer : middle : connection refused",
			wantTarget:  sentinel,
		},
		{
			name:        "fmt verbs in contextual message stay literal",
			contextual:  "boom %s %w",
			inner:       sentinel,
			wantMessage: "boom %s %w : connection refused",
			wantTarget:  sentinel,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := WrapError(tc.contextual, tc.inner)
			require.Error(t, err)
			assert.Equal(t, tc.wantMessage, err.Error())
			if tc.wantTarget != nil {
				assert.True(t, errors.Is(err, tc.wantTarget))
			}
		})
	}
}
