// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package parser

import (
	"context"
	"os"
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

// The common case: `config generate` collected the cluster name already,
// and one cluster is all the operator has — bootstrap must not demand the
// name a second time.
func TestResolveConfigsDirectoryAutoSelectsTheOnlySavedCluster(t *testing.T) {
	resetFlags(t)

	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	dir := filepath.Join(configHome, "kubeaid-cli", "prod-eu", "configs")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "general.yaml"), []byte("cluster:\n  name: prod-eu\n"), 0o600))

	require.NoError(t, ResolveConfigsDirectory(context.Background()))

	require.Equal(t, dir, globals.ConfigsDirectory)
}

// There is no implicit default: naming neither a directory nor a cluster is
// refused, with the two flags spelled out so the fix is a copy-paste.
func TestResolveConfigsDirectoryRefusesToGuessWithoutAnySource(t *testing.T) {
	resetFlags(t)

	err := ResolveConfigsDirectory(context.Background())

	require.Error(t, err)
	require.Contains(t, err.Error(), constants.FlagNameClusterName)
	require.Contains(t, err.Error(), constants.FlagNameConfigsDirectory)
}

// The refusal exists to hand the operator a copy-paste fix, so when clusters
// have been saved, their names must actually appear in the error.
func TestResolveConfigsDirectoryRefusalListsTheSavedClusters(t *testing.T) {
	resetFlags(t)

	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	for _, cluster := range []string{"prod-eu", "staging-01"} {
		dir := filepath.Join(configHome, "kubeaid-cli", cluster, "configs")
		require.NoError(t, os.MkdirAll(dir, 0o700))
		require.NoError(t, os.WriteFile(
			filepath.Join(dir, "general.yaml"), []byte("cluster:\n  name: "+cluster+"\n"), 0o600))
	}

	err := ResolveConfigsDirectory(context.Background())

	require.Error(t, err)
	require.Contains(t, err.Error(), "prod-eu")
	require.Contains(t, err.Error(), "staging-01")
}

func TestUsingDefaultConfigsDirectory(t *testing.T) {
	resetFlags(t)
	require.True(t, UsingDefaultConfigsDirectory())

	globals.ConfigsDirectory = "/somewhere/else"
	require.False(t, UsingDefaultConfigsDirectory())
}
