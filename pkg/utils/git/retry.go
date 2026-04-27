// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package git

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/go-git/go-git/v5/plumbing/transport"
)

var (
	gitOperationMaxAttempts       = 5
	gitOperationInitialRetryDelay = 2 * time.Second
	gitOperationMaxRetryDelay     = 15 * time.Second
)

func retryGitOperation(ctx context.Context, operation string, fn func() error) error {
	var err error
	retryDelay := gitOperationInitialRetryDelay

	for attempt := 1; attempt <= gitOperationMaxAttempts; attempt++ {
		err = fn()
		if err == nil {
			return nil
		}

		if !shouldRetryGitError(err) || attempt == gitOperationMaxAttempts {
			return err
		}

		slog.WarnContext(ctx,
			"Git operation failed; retrying",
			slog.String("operation", operation),
			slog.Int("attempt", attempt),
			slog.Int("max-attempts", gitOperationMaxAttempts),
			slog.Duration("retry-in", retryDelay),
			slog.String("error", err.Error()),
		)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(retryDelay):
		}

		retryDelay *= 2
		if retryDelay > gitOperationMaxRetryDelay {
			retryDelay = gitOperationMaxRetryDelay
		}
	}

	return err
}

func retryGitOperationWithResult[T any](
	ctx context.Context,
	operation string,
	fn func() (T, error),
) (T, error) {
	var result T
	err := retryGitOperation(ctx, operation, func() error {
		var err error
		result, err = fn()
		return err
	})

	return result, err
}

func shouldRetryGitError(err error) bool {
	if err == nil {
		return false
	}

	switch {
	case errors.Is(err, context.Canceled),
		errors.Is(err, context.DeadlineExceeded),
		errors.Is(err, transport.ErrAuthenticationRequired),
		errors.Is(err, transport.ErrAuthorizationFailed),
		errors.Is(err, transport.ErrEmptyRemoteRepository):
		return false
	}

	errorMessage := strings.ToLower(err.Error())
	retryableErrors := []string{
		"ssh: handshake failed",
		"failed to sign challenge",
		"agent refused operation",
		"sign_and_send_pubkey",
		"unexpected eof",
		"i/o timeout",
		"connection reset by peer",
		"temporarily unavailable",
		"connection refused",
		"broken pipe",
	}

	for _, marker := range retryableErrors {
		if strings.Contains(errorMessage, marker) {
			return true
		}
	}

	return false
}
