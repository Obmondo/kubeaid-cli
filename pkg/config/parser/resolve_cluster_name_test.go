// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package parser

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/globals"
)

// resetFlags restores the globals both flags write into, so one case cannot
// leak its directory into the next.
func resetFlags(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	configsDirectory, clusterName := globals.ConfigsDirectory, globals.ClusterName
	t.Cleanup(func() {
		globals.ConfigsDirectory, globals.ClusterName = configsDirectory, clusterName
	})

	globals.ConfigsDirectory = constants.FlagNameConfigsDirectoryDefaultValue
	globals.ClusterName = ""
}

func TestResolveConfigsDirectoryUsesClusterNameWhenTheDirectoryIsDefault(t *testing.T) {
	resetFlags(t)
	globals.ClusterName = "demo-01"

	require.NoError(t, ResolveConfigsDirectory(context.Background()))

	require.Contains(t, globals.ConfigsDirectory, filepath.Join("kubeaid-cli", "demo-01"))
	require.True(t, filepath.IsAbs(globals.ConfigsDirectory))
}

// The two flags must not fight: an operator who spelled out a path meant it,
// even when a cluster name is also floating around from a shell alias or
// from --cluster-name on the same line.
func TestResolveConfigsDirectoryLetsAnExplicitDirectoryWin(t *testing.T) {
	resetFlags(t)
	globals.ClusterName = "demo-01"
	globals.ConfigsDirectory = "/explicit/path"

	require.NoError(t, ResolveConfigsDirectory(context.Background()))

	require.Equal(t, "/explicit/path", globals.ConfigsDirectory)
}

// Without --cluster-name the old working-directory default has to survive
// untouched, or every existing operator's invocation changes meaning.
func TestResolveConfigsDirectoryLeavesTheDefaultAloneWithoutAClusterName(t *testing.T) {
	resetFlags(t)

	require.NoError(t, ResolveConfigsDirectory(context.Background()))

	require.Equal(t, constants.FlagNameConfigsDirectoryDefaultValue, globals.ConfigsDirectory)
}

func TestUsingDefaultConfigsDirectory(t *testing.T) {
	resetFlags(t)
	require.True(t, UsingDefaultConfigsDirectory())

	globals.ConfigsDirectory = "/somewhere/else"
	require.False(t, UsingDefaultConfigsDirectory())
}
