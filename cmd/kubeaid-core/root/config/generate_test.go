// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/globals"
)

const (
	// flagClusterName is what --cluster-name carries and pickedClusterName
	// what the picker returns — two distinct clusters, so no case can pass
	// by the two happening to agree.
	flagClusterName   = "demo-01"
	pickedClusterName = "demo-02"
)

// promptRecorder stands in for the picker resolveTargetCluster reaches,
// answering with a canned value and recording that it was asked.
type promptRecorder struct {
	picked string
	asks   int
}

func (r *promptRecorder) install(t *testing.T) {
	t.Helper()

	ask := askTargetCluster
	t.Cleanup(func() { askTargetCluster = ask })

	askTargetCluster = func() (string, error) {
		r.asks++
		return r.picked, nil
	}
}

// freshFlagState gives the test its own config home, stubs the picker, and
// restores the globals both flags write into, so one case cannot leak into
// the next.
func freshFlagState(t *testing.T) (string, *promptRecorder) {
	t.Helper()

	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)

	configsDirectory, clusterName := globals.ConfigsDirectory, globals.ClusterName
	t.Cleanup(func() {
		globals.ConfigsDirectory, globals.ClusterName = configsDirectory, clusterName
	})

	globals.ConfigsDirectory = constants.FlagNameConfigsDirectoryDefaultValue
	globals.ClusterName = ""

	recorder := &promptRecorder{}
	recorder.install(t)

	return home, recorder
}

// huh's Options() positions both the cursor and the options scroll offset by
// matching each option's value against the accessor, which reads as the zero
// string before a value is set. A "" sentinel matched "+ new cluster", parking
// the offset on it, so the saved clusters rendered off-screen above — the list
// only appeared once a keypress recomputed the offset.
//
// A rendering bug no unit test can see, so guard the property that caused it.
func TestNewClusterOptionValueIsNotTheZeroString(t *testing.T) {
	assert.NotEmpty(t, newClusterOptionValue,
		"an empty sentinel collides with an unset huh accessor")

	// It must also not collide with a name clusterdir could return, or picking
	// that cluster would be read as "+ new cluster".
	assert.Contains(t, newClusterOptionValue, "\x00",
		"a sentinel a directory name could equal is not a sentinel")
}

func TestResolveTargetClusterLetsAnExplicitDirectoryWin(t *testing.T) {
	_, recorder := freshFlagState(t)
	globals.ConfigsDirectory = "/explicit/path"
	globals.ClusterName = flagClusterName

	clusterName, err := resolveTargetCluster()
	require.NoError(t, err)

	// "" so printNextStep omits --cluster-name: the operator spelled out a
	// path, and that is what cluster bootstrap has to be pointed at too.
	assert.Empty(t, clusterName)
	assert.Equal(t, "/explicit/path", globals.ConfigsDirectory)
	assert.Zero(t, recorder.asks)
}

func TestResolveTargetClusterUsesTheClusterNameFlag(t *testing.T) {
	home, recorder := freshFlagState(t)
	globals.ClusterName = flagClusterName

	clusterName, err := resolveTargetCluster()
	require.NoError(t, err)

	assert.Equal(t, flagClusterName, clusterName)
	assert.Equal(t,
		filepath.Join(home, "kubeaid-cli", flagClusterName, "configs"),
		globals.ConfigsDirectory,
	)
	assert.Zero(t, recorder.asks)
}

func TestResolveTargetClusterAsksThePickerWithNoFlags(t *testing.T) {
	home, recorder := freshFlagState(t)
	recorder.picked = pickedClusterName

	clusterName, err := resolveTargetCluster()
	require.NoError(t, err)

	assert.Equal(t, 1, recorder.asks)
	assert.Equal(t, pickedClusterName, clusterName)
	assert.Equal(t,
		filepath.Join(home, "kubeaid-cli", pickedClusterName, "configs"),
		globals.ConfigsDirectory,
	)
}

// A config sitting in the working directory's old default location is never
// picked up implicitly — an operator who wants a custom location says so with
// --configs-directory. The picker is asked as if the directory were not there.
func TestResolveTargetClusterIgnoresAWorkingDirectoryConfig(t *testing.T) {
	home, recorder := freshFlagState(t)
	recorder.picked = pickedClusterName

	workingDirectory := t.TempDir()
	t.Chdir(workingDirectory)
	stale := filepath.Join(workingDirectory, "outputs", "configs")
	require.NoError(t, os.MkdirAll(stale, 0o700))
	require.NoError(t, os.WriteFile(
		filepath.Join(stale, "general.yaml"),
		[]byte("cluster:\n  name: old-cluster\n"), 0o600))

	clusterName, err := resolveTargetCluster()
	require.NoError(t, err)

	assert.Equal(t, 1, recorder.asks)
	assert.Equal(t, pickedClusterName, clusterName)
	assert.Equal(t,
		filepath.Join(home, "kubeaid-cli", pickedClusterName, "configs"),
		globals.ConfigsDirectory,
	)
}
