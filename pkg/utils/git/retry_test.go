// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package git

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/stretchr/testify/assert"
)

func TestShouldRetryGitError(t *testing.T) {
	testCases := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "ssh sign challenge failure",
			err:      errors.New("ssh: handshake failed: agent: failed to sign challenge"),
			expected: true,
		},
		{
			name:     "unexpected EOF",
			err:      errors.New("unexpected EOF"),
			expected: true,
		},
		{
			name:     "io timeout",
			err:      errors.New("dial tcp: i/o timeout"),
			expected: true,
		},
		{
			name:     "authentication required",
			err:      fmt.Errorf("wrapped: %w", transport.ErrAuthenticationRequired),
			expected: false,
		},
		{
			name:     "authorization failed",
			err:      transport.ErrAuthorizationFailed,
			expected: false,
		},
		{
			name:     "empty remote repo",
			err:      transport.ErrEmptyRemoteRepository,
			expected: false,
		},
		{
			name:     "random error",
			err:      errors.New("invalid tag name"),
			expected: false,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.expected, shouldRetryGitError(testCase.err))
		})
	}
}

func TestRetryGitOperationRetriesRetryableErrors(t *testing.T) {
	originalInitialDelay := gitOperationInitialRetryDelay
	originalMaxDelay := gitOperationMaxRetryDelay
	originalMaxAttempts := gitOperationMaxAttempts
	t.Cleanup(func() {
		gitOperationInitialRetryDelay = originalInitialDelay
		gitOperationMaxRetryDelay = originalMaxDelay
		gitOperationMaxAttempts = originalMaxAttempts
	})

	gitOperationInitialRetryDelay = time.Millisecond
	gitOperationMaxRetryDelay = time.Millisecond
	gitOperationMaxAttempts = 5

	attemptCount := 0
	err := retryGitOperation(context.Background(), "test", func() error {
		attemptCount++
		if attemptCount < 3 {
			return errors.New("ssh: handshake failed: agent: failed to sign challenge")
		}
		return nil
	})
	assert.NoError(t, err)
	assert.Equal(t, 3, attemptCount)
}

func TestRetryGitOperationDoesNotRetryNonRetryableErrors(t *testing.T) {
	originalInitialDelay := gitOperationInitialRetryDelay
	originalMaxDelay := gitOperationMaxRetryDelay
	originalMaxAttempts := gitOperationMaxAttempts
	t.Cleanup(func() {
		gitOperationInitialRetryDelay = originalInitialDelay
		gitOperationMaxRetryDelay = originalMaxDelay
		gitOperationMaxAttempts = originalMaxAttempts
	})

	gitOperationInitialRetryDelay = time.Millisecond
	gitOperationMaxRetryDelay = time.Millisecond
	gitOperationMaxAttempts = 5

	attemptCount := 0
	err := retryGitOperation(context.Background(), "test", func() error {
		attemptCount++
		return transport.ErrAuthenticationRequired
	})
	assert.ErrorIs(t, err, transport.ErrAuthenticationRequired)
	assert.Equal(t, 1, attemptCount)
}
