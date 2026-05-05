// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package keycloak

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateAdminPassword(t *testing.T) {
	t.Parallel()

	pw, err := GenerateAdminPassword()
	require.NoError(t, err)

	assert.Len(t, pw, passwordLength)

	for i, r := range pw {
		assert.True(t, strings.ContainsRune(passwordAlphabet, r),
			"position %d: %q is not in the password alphabet", i, r)
	}
}
