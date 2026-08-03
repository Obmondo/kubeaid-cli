// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package clusterdir

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// withConfigHome points os.UserConfigDir at a temp tree for the test.
func withConfigHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)
	return home
}

// saveConfig creates the tree a real config would occupy.
func saveConfig(t *testing.T, home, cluster string) {
	t.Helper()
	dir := filepath.Join(home, dirName, cluster, configsSubdir)
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, generalFileName), []byte("cluster:\n  name: "+cluster+"\n"), 0o600))
}

func TestForIsPerClusterAndAbsolute(t *testing.T) {
	withConfigHome(t)

	dir, err := For("demo-01")
	require.NoError(t, err)

	require.Contains(t, dir, filepath.Join(dirName, "demo-01"))

	// A working-directory-relative path is the thing this exists to avoid:
	// it would put secrets.yaml inside whatever checkout the operator is in.
	require.True(t, filepath.IsAbs(dir))
}

func TestListReturnsSavedClustersSorted(t *testing.T) {
	home := withConfigHome(t)
	saveConfig(t, home, "prod-01")
	saveConfig(t, home, "demo-01")

	require.Equal(t, []string{"demo-01", "prod-01"}, List())
}

// An aborted run can leave the tree behind with no config in it. Offering
// that name back would send the operator at a directory that cannot be used.
func TestListSkipsDirectoriesWithNoConfig(t *testing.T) {
	home := withConfigHome(t)
	saveConfig(t, home, "real-01")
	require.NoError(t, os.MkdirAll(filepath.Join(home, dirName, "abandoned", configsSubdir), 0o700))

	require.Equal(t, []string{"real-01"}, List())
}

// List only ever decorates a message the operator is already reading, so a
// missing or unreadable root must not turn into an error of its own.
func TestListIsEmptyWhenNothingHasBeenSaved(t *testing.T) {
	withConfigHome(t)
	require.Empty(t, List())
}
