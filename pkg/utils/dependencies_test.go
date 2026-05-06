// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package utils

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVerifyExecutableInPathUsesInjectedLookPath(t *testing.T) {
	lookedUp := ""
	lookPath := func(file string) (string, error) {
		lookedUp = file
		return "/usr/bin/" + file, nil
	}

	err := verifyExecutableInPath("kubectl", lookPath)

	require.NoError(t, err)
	assert.Equal(t, "kubectl", lookedUp)
}

func TestEnsureRuntimeDependencyInstalledReturnsErrorForMissingExecutable(t *testing.T) {
	err := EnsureRuntimeDependencyInstalled(context.Background(), "definitely-not-a-kubeaid-runtime-dependency")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "runtime dependency unavailable")
	assert.Contains(t, err.Error(), "definitely-not-a-kubeaid-runtime-dependency")
}
