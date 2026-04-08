// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package utils

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWithRetry(t *testing.T) {
	callCount := 0
	err := WithRetry(time.Millisecond, 3, func() error {
		callCount++
		if callCount < 3 {
			return errors.New("not yet")
		}
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, 3, callCount)
}

func TestWithRetrySucceedsFirstTry(t *testing.T) {
	err := WithRetry(time.Millisecond, 5, func() error {
		return nil
	})
	require.NoError(t, err)
}

func TestWithRetryExhausted(t *testing.T) {
	err := WithRetry(time.Millisecond, 3, func() error {
		return errors.New("always fails")
	})
	require.Error(t, err)
	assert.Equal(t, "always fails", err.Error())
}

func TestWithRetryZeroAttempts(t *testing.T) {
	called := false
	err := WithRetry(time.Millisecond, 0, func() error {
		called = true
		return errors.New("should not reach here")
	})

	assert.False(t, called)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "0 attempts")
}

func TestWithRetryDoesNotSleepAfterLastAttempt(t *testing.T) {
	start := time.Now()
	_ = WithRetry(100*time.Millisecond, 2, func() error {
		return errors.New("fail")
	})
	elapsed := time.Since(start)

	// should sleep once (between attempt 1 and 2), not twice
	assert.Less(t, elapsed, 250*time.Millisecond)
}
