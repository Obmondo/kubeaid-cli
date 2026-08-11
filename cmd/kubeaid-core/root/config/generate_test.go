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
	// flagClusterName is what --cluster-name carries, pickedClusterName what
	// the picker returns, and staleClusterName what a config already sitting
	// in the default directory names — three distinct clusters, so no case
	// can pass by two of them happening to agree.
	flagClusterName   = "demo-01"
	pickedClusterName = "demo-02"
	staleClusterName  = "old-cluster"

	staleGeneralConfig = "cluster:\n  name: " + staleClusterName + "\n"
)

// promptRecorder stands in for the two TUI entry points resolveTargetCluster
// reaches, answering with canned values and recording what was asked.
type promptRecorder struct {
	reuse  bool
	picked string

	confirms     int
	confirmedFor string
	asks         int
}

func (r *promptRecorder) install(t *testing.T) {
	t.Helper()

	confirm, ask := confirmReuseDefaultConfigsDirectory, askTargetCluster
	t.Cleanup(func() {
		confirmReuseDefaultConfigsDirectory, askTargetCluster = confirm, ask
	})

	confirmReuseDefaultConfigsDirectory = func(configsDirectory string) (bool, error) {
		r.confirms++
		r.confirmedFor = configsDirectory
		return r.reuse, nil
	}
	askTargetCluster = func() (string, error) {
		r.asks++
		return r.picked, nil
	}
}

// freshFlagState gives the test its own config home, stubs the prompts, and
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

// defaultConfigsDirectoryHolding plants files in the default configs
// directory of a working directory the test then runs from.
func defaultConfigsDirectoryHolding(t *testing.T, files map[string]string) string {
	t.Helper()

	workingDirectory := t.TempDir()
	t.Chdir(workingDirectory)

	directory := filepath.Join(workingDirectory, constants.FlagNameConfigsDirectoryDefaultValue)
	require.NoError(t, os.MkdirAll(directory, 0o700))
	for name, contents := range files {
		require.NoError(t, os.WriteFile(filepath.Join(directory, name), []byte(contents), 0o600))
	}

	return workingDirectory
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
	assert.Zero(t, recorder.confirms)
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
	assert.Zero(t, recorder.confirms)
	assert.Zero(t, recorder.asks)
}

// The fix: with no flags the default directory is always what gets looked at,
// so a config already in it is offered rather than taken. Reusing it stays
// available — it is just a decision now.
func TestResolveTargetClusterOffersTheDefaultDirectoryRatherThanReusingIt(t *testing.T) {
	_, recorder := freshFlagState(t)
	recorder.reuse = true
	defaultConfigsDirectoryHolding(t, map[string]string{
		"general.yaml": staleGeneralConfig,
	})

	clusterName, err := resolveTargetCluster()
	require.NoError(t, err)

	assert.Equal(t, 1, recorder.confirms)
	assert.Equal(t, constants.FlagNameConfigsDirectoryDefaultValue, recorder.confirmedFor)
	assert.Zero(t, recorder.asks)

	assert.Empty(t, clusterName)
	assert.Equal(t, constants.FlagNameConfigsDirectoryDefaultValue, globals.ConfigsDirectory)
}

// Declining is what the old code gave nobody a way to do: the run moves to the
// picker and writes under the per-cluster convention, leaving the stale config
// where it was.
func TestResolveTargetClusterFallsBackToThePickerWhenTheDefaultIsDeclined(t *testing.T) {
	home, recorder := freshFlagState(t)
	recorder.reuse, recorder.picked = false, pickedClusterName
	workingDirectory := defaultConfigsDirectoryHolding(t, map[string]string{
		"general.yaml": staleGeneralConfig,
	})

	clusterName, err := resolveTargetCluster()
	require.NoError(t, err)

	assert.Equal(t, 1, recorder.confirms)
	assert.Equal(t, 1, recorder.asks)

	assert.Equal(t, pickedClusterName, clusterName)
	assert.Equal(t,
		filepath.Join(home, "kubeaid-cli", pickedClusterName, "configs"),
		globals.ConfigsDirectory,
	)

	stale, err := os.ReadFile(filepath.Join(
		workingDirectory, constants.FlagNameConfigsDirectoryDefaultValue, "general.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(stale), staleClusterName)
}

// An aborted run leaves the directory behind with nothing in it. There is
// nothing to reuse, so asking would be a question about nothing.
func TestResolveTargetClusterDoesNotOfferAnEmptyDefaultDirectory(t *testing.T) {
	home, recorder := freshFlagState(t)
	recorder.picked = pickedClusterName
	defaultConfigsDirectoryHolding(t, nil)

	clusterName, err := resolveTargetCluster()
	require.NoError(t, err)

	assert.Zero(t, recorder.confirms)
	assert.Equal(t, 1, recorder.asks)

	assert.Equal(t, pickedClusterName, clusterName)
	assert.Equal(t,
		filepath.Join(home, "kubeaid-cli", pickedClusterName, "configs"),
		globals.ConfigsDirectory,
	)
}

// An interrupted run left only its prompt state — still resumable, so it has
// to be offered like a full config.
func TestResolveTargetClusterOffersADirectoryHoldingOnlyPromptState(t *testing.T) {
	_, recorder := freshFlagState(t)
	recorder.reuse = true
	defaultConfigsDirectoryHolding(t, map[string]string{
		".kubeaid-prompt-state.yaml": "basics: true\n",
	})

	clusterName, err := resolveTargetCluster()
	require.NoError(t, err)

	assert.Equal(t, 1, recorder.confirms)
	assert.Empty(t, clusterName)
	assert.Equal(t, constants.FlagNameConfigsDirectoryDefaultValue, globals.ConfigsDirectory)
}

// --cluster-name is checked first, so naming a cluster skips the question
// entirely rather than being asked about a directory it is not using.
func TestResolveTargetClusterFlagBeatsTheDefaultDirectory(t *testing.T) {
	home, recorder := freshFlagState(t)
	globals.ClusterName = flagClusterName
	defaultConfigsDirectoryHolding(t, map[string]string{
		"general.yaml": staleGeneralConfig,
	})

	clusterName, err := resolveTargetCluster()
	require.NoError(t, err)

	assert.Zero(t, recorder.confirms)
	assert.Zero(t, recorder.asks)

	assert.Equal(t, flagClusterName, clusterName)
	assert.Equal(t,
		filepath.Join(home, "kubeaid-cli", flagClusterName, "configs"),
		globals.ConfigsDirectory,
	)
}

// The name is what makes the question answerable, but it only decorates a
// prompt — an unreadable config must still leave something to answer.
func TestClusterNameInConfigsDirectory(t *testing.T) {
	tests := []struct {
		name     string
		general  *string
		expected string
	}{
		{name: "names the cluster", general: ptr(staleGeneralConfig), expected: staleClusterName},
		{name: "no general.yaml", general: nil, expected: ""},
		{name: "malformed yaml", general: ptr("cluster: [unterminated\n"), expected: ""},
		{name: "no cluster block", general: ptr("forks:\n  kubeaid:\n    url: x\n"), expected: ""},
		{name: "cluster block without a name", general: ptr("cluster:\n  type: workload\n"), expected: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			directory := t.TempDir()
			if tc.general != nil {
				require.NoError(t, os.WriteFile(
					filepath.Join(directory, "general.yaml"), []byte(*tc.general), 0o600))
			}

			assert.Equal(t, tc.expected, clusterNameInConfigsDirectory(directory))
		})
	}
}

func ptr(s string) *string { return &s }
