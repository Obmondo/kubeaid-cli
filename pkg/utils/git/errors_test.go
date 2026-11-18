// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package git

import (
	"errors"
	"fmt"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// assertGitFailure asserts that err is a *Error with the given headline and, when any are given,
// exactly the given attributes.
func assertGitFailure(t *testing.T, err error, headline string, wantAttrs ...slog.Attr) {
	t.Helper()

	var gitErr *Error
	require.ErrorAs(t, err, &gitErr)
	assert.Equal(t, headline, gitErr.Message)

	if len(wantAttrs) > 0 {
		assert.ElementsMatch(t, wantAttrs, gitErr.Attrs)
	}
}

func TestFail(t *testing.T) {
	cause := errors.New("connection refused")

	tests := []struct {
		name         string
		err          error
		attrs        []slog.Attr
		wantNil      bool
		wantErrorMsg string
	}{
		{
			name:    "a nil error stays nil",
			err:     nil,
			wantNil: true,
		},
		{
			name:         "a failure keeps its headline and its cause",
			err:          cause,
			wantErrorMsg: "Failed pushing commit to upstream: connection refused",
		},
		{
			name:         "a failure keeps its attributes",
			err:          cause,
			attrs:        []slog.Attr{slog.String("ref", "v1.2.3")},
			wantErrorMsg: "Failed pushing commit to upstream: connection refused",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := fail("Failed pushing commit to upstream", tc.err, tc.attrs...)
			if tc.wantNil {
				assert.NoError(t, err)
				return
			}

			assertGitFailure(t, err, "Failed pushing commit to upstream", tc.attrs...)
			require.ErrorIs(t, err, cause)
			assert.EqualError(t, err, tc.wantErrorMsg)
		})
	}
}

func TestErrorWithoutACause(t *testing.T) {
	err := &Error{Message: "Failed pushing commit to upstream"}

	assert.EqualError(t, err, "Failed pushing commit to upstream")
	assert.NoError(t, err.Unwrap())
}

func TestAnnotate(t *testing.T) {
	attr := slog.String("ref", "v1.2.3")
	cause := errors.New("reference not found")

	tests := []struct {
		name string
		err  error
		want []slog.Attr
	}{
		{name: "no error", err: nil},
		{name: "an error that is not a git error", err: cause},
		{
			name: "a git error",
			err:  fail("Failed resolving provided git ref", cause),
			want: []slog.Attr{attr},
		},
		{
			name: "a wrapped git error",
			err:  fmt.Errorf("resolving: %w", fail("Failed resolving provided git ref", cause)),
			want: []slog.Attr{attr},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.err
			annotate(&err, attr)

			var gitErr *Error
			if !errors.As(err, &gitErr) {
				assert.Empty(t, tc.want)
				return
			}
			assert.Equal(t, tc.want, gitErr.Attrs)
		})
	}
}
