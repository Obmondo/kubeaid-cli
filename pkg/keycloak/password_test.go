// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package keycloak

import (
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
