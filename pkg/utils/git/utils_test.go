// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package git

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsHTTPURL(t *testing.T) {
	assert.True(t, isHTTPURL("https://github.com/org/repo.git"))
	assert.True(t, isHTTPURL("http://gitea.local:3000/org/repo.git"))
	assert.False(t, isHTTPURL("git@github.com:org/repo.git"))
	assert.False(t, isHTTPURL("ssh://git@github.com/org/repo.git"))
	assert.False(t, isHTTPURL(""))
}
