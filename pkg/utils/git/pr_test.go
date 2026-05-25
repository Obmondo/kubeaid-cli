// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package git

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestIsGPGSigningError pins the contract between gpgAgentSigner.Sign
// (which wraps its failures with the "gpg signing failed" prefix) and
// commitWithGPGRetry's gate. A typo on either side would silently
// disable the interactive YubiKey-retry prompt; this test fails loudly
// instead.
func TestIsGPGSigningError(t *testing.T) {
	testCases := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil",
			err:  nil,
			want: false,
		},
		{
			name: "card error from yubikey",
			err: fmt.Errorf(
				"gpg signing failed: exit status 2 (gpg stderr: gpg: signing failed: Card error)",
			),
			want: true,
		},
		{
			name: "wrapped gpg signer error",
			err: fmt.Errorf("commit: %w",
				errors.New("gpg signing failed: exit status 2 (gpg stderr: gpg: signing failed: No pinentry)")),
			want: true,
		},
		{
			name: "unrelated commit failure",
			err:  errors.New("worktree: dirty index"),
			want: false,
		},
		{
			name: "ssh push failure should not match",
			err:  errors.New("ssh: handshake failed: agent refused operation"),
			want: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isGPGSigningError(tc.err))
		})
	}
}
