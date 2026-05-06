// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package hetzner

import (
	"errors"
	"fmt"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAsDNSErr_DirectMatch(t *testing.T) {
	t.Parallel()

	original := &net.DNSError{Err: "no such host", IsNotFound: true}

	var got *net.DNSError
	assert.True(t, asDNSErr(original, &got))
	assert.True(t, got.IsNotFound)
}

func TestAsDNSErr_WrappedOnce(t *testing.T) {
	t.Parallel()

	// fmt.Errorf with %w preserves the underlying *net.DNSError so
	// callers wrapping the lookup error in their own context still
	// let lookupA detect NXDOMAIN as "" rather than as a hard
	// failure.
	original := &net.DNSError{Err: "no such host", IsNotFound: true}
	wrapped := fmt.Errorf("lookup keycloak.acme.com: %w", original)

	var got *net.DNSError
	assert.True(t, asDNSErr(wrapped, &got))
	assert.True(t, got.IsNotFound)
}

func TestAsDNSErr_UnwrappableNonDNS(t *testing.T) {
	t.Parallel()

	// A plain error chain that never reaches a *net.DNSError
	// returns false; the caller treats it as a transient failure
	// that the next poll tick will retry.
	wrapped := fmt.Errorf("dial timeout: %w", errors.New("i/o timeout"))

	var got *net.DNSError
	assert.False(t, asDNSErr(wrapped, &got))
	assert.Nil(t, got)
}

func TestAsDNSErr_NilError(t *testing.T) {
	t.Parallel()

	var got *net.DNSError
	assert.False(t, asDNSErr(nil, &got))
	assert.Nil(t, got)
}
