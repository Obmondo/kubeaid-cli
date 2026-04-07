// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package utils

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWrapError(t *testing.T) {
	inner := errors.New("connection refused")
	wrapped := WrapError("failed to reach API", inner)

	assert.Equal(t, "failed to reach API : connection refused", wrapped.Error())

	// the original error should be unwrappable
	require.True(t, errors.Is(wrapped, inner))
}
