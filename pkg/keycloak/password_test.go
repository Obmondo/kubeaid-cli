// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package keycloak

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeneratePassword_Format(t *testing.T) {
	t.Parallel()

	pw, err := generatePassword()
	require.NoError(t, err)

	assert.Len(t, pw, passwordLength,
		"generated password must be exactly passwordLength runes")

	for i, r := range pw {
		assert.True(t, strings.ContainsRune(passwordAlphabet, r),
			"position %d: %q is not in the password alphabet", i, r)
	}
}

func TestGeneratePassword_Uniqueness(t *testing.T) {
	t.Parallel()

	// Two consecutive calls must produce different strings. With
	// 32 alphanumerics (~190 bits) the collision probability is
	// astronomically small — a collision here would indicate a
	// broken RNG, not bad luck.
	a, err := generatePassword()
	require.NoError(t, err)

	b, err := generatePassword()
	require.NoError(t, err)

	assert.NotEqual(t, a, b)
}

func TestGenerateBase64Key_DecodesToRequestedByteLength(t *testing.T) {
	t.Parallel()

	tests := []int{16, 32, 64}
	for _, byteLen := range tests {
		key, err := generateBase64Key(byteLen)
		require.NoError(t, err)

		raw, err := base64.StdEncoding.DecodeString(key)
		require.NoError(t, err,
			"generateBase64Key(%d) must produce valid standard-base64", byteLen)
		assert.Len(t, raw, byteLen,
			"decoded key length must match requested byte length")
	}
}

func TestGenerateBase64Key_Uniqueness(t *testing.T) {
	t.Parallel()

	a, err := generateBase64Key(32)
	require.NoError(t, err)

	b, err := generateBase64Key(32)
	require.NoError(t, err)

	assert.NotEqual(t, a, b,
		"two consecutive calls to generateBase64Key must produce different output")
}
