// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package core

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRetryKeycloakReconcile(t *testing.T) {
	// Shrink the retry tunables so the failure path runs sub-second.
	// Mutates package-level vars — not t.Parallel().
	origAttempts, origInterval := keycloakReconcileMaxAttempts, keycloakReconcileRetryInterval
	t.Cleanup(func() {
		keycloakReconcileMaxAttempts = origAttempts
		keycloakReconcileRetryInterval = origInterval
	})
	keycloakReconcileMaxAttempts = 3
	keycloakReconcileRetryInterval = time.Millisecond

	t.Run("succeeds on the first attempt", func(t *testing.T) {
		calls := 0
		err := retryKeycloakReconcile(context.Background(), func(context.Context) error {
			calls++
			return nil
		})
		require.NoError(t, err)
		assert.Equal(t, 1, calls)
	})

	t.Run("retries until an attempt succeeds", func(t *testing.T) {
		calls := 0
		err := retryKeycloakReconcile(context.Background(), func(context.Context) error {
			calls++
			if calls < keycloakReconcileMaxAttempts {
				return errors.New("transient port-forward EOF")
			}
			return nil
		})
		require.NoError(t, err)
		assert.Equal(t, keycloakReconcileMaxAttempts, calls)
	})

	t.Run("gives up after maxAttempts, wrapping the last error", func(t *testing.T) {
		calls := 0
		err := retryKeycloakReconcile(context.Background(), func(context.Context) error {
			calls++
			return fmt.Errorf("attempt %d failed", calls)
		})
		require.Error(t, err)
		assert.Equal(t, keycloakReconcileMaxAttempts, calls)
		assert.Contains(t, err.Error(), "after 3 attempts")
		assert.Contains(t, err.Error(), "attempt 3 failed")
	})

	t.Run("aborts when the context is cancelled", func(t *testing.T) {
		// A long interval guarantees the select blocks on the timer, so
		// it's the cancelled ctx that unblocks it — not the interval
		// elapsing — making the assertion deterministic.
		keycloakReconcileRetryInterval = time.Hour

		ctx, cancel := context.WithCancel(context.Background())
		calls := 0
		err := retryKeycloakReconcile(ctx, func(context.Context) error {
			calls++
			cancel() // cancel after the first failed attempt
			return errors.New("fail")
		})
		require.ErrorIs(t, err, context.Canceled)
		assert.Equal(t, 1, calls)
	})
}
