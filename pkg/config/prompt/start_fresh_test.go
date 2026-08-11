// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package prompt

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubLoadExistingConfirm answers the "Load existing / Start fresh" question
// without a TTY.
func stubLoadExistingConfirm(t *testing.T, loadExisting bool) {
	t.Helper()

	original := confirmLoadExistingConfig
	t.Cleanup(func() { confirmLoadExistingConfig = original })

	confirmLoadExistingConfig = func(string) (bool, error) {
		return loadExisting, nil
	}
}

// sessionOver builds a session on a directory already holding oldCluster's
// config, pre-filled the way ConfigFromPrompt pre-fills it.
func sessionOver(t *testing.T, oldCluster string) *promptSession {
	t.Helper()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "general.yaml"),
		[]byte("cluster:\n  name: "+oldCluster+"\n  type: workload\n"),
		0o600,
	))

	return &promptSession{
		configsDirectory: dir,
		detected:         &autoDetectedConfig{},
		cfg:              &PromptedConfig{ClusterName: oldCluster},
		state:            &promptState{},
	}
}

// Start fresh has to put the cluster name question back on the screen. Leaving
// the old name pre-filled meant the operator who asked not to continue this
// config was handed its cluster back as the default answer.
func TestStartFreshClearsThePreFilledClusterName(t *testing.T) {
	stubLoadExistingConfirm(t, false)
	session := sessionOver(t, "old-cluster")

	require.NoError(t, session.loadExistingConfigIfRequested())

	assert.Empty(t, session.cfg.ClusterName)
	// Nothing was loaded, so every step is still open.
	assert.Equal(t, promptState{}, *session.state)
}

func TestLoadExistingKeepsTheClusterName(t *testing.T) {
	stubLoadExistingConfirm(t, true)
	session := sessionOver(t, "old-cluster")

	require.NoError(t, session.loadExistingConfigIfRequested())

	assert.Equal(t, "old-cluster", session.cfg.ClusterName)
}

func TestResolveWriteTarget(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)

	tests := []struct {
		name        string
		directory   string
		picked      string
		final       string
		wantDirFunc func() string
		wantCluster string
	}{
		{
			// --configs-directory or a legacy outputs/configs: the operator
			// chose the directory by hand, so the name does not move it, and
			// bootstrap needs the path rather than --cluster-name.
			name:        "no convention in play leaves the directory alone",
			directory:   "/explicit/path",
			picked:      "",
			final:       "whatever-they-typed",
			wantDirFunc: func() string { return "/explicit/path" },
			wantCluster: "",
		},
		{
			name:        "same name keeps the directory",
			directory:   filepath.Join(home, "kubeaid-cli", "demo-01", "configs"),
			picked:      "demo-01",
			final:       "demo-01",
			wantDirFunc: func() string { return filepath.Join(home, "kubeaid-cli", "demo-01", "configs") },
			wantCluster: "demo-01",
		},
		{
			// The fix: a fresh start under old-cluster's directory that ends
			// up named demo-02 belongs in demo-02's directory, not on top of
			// old-cluster's config.
			name:        "a new name moves the config to its own directory",
			directory:   filepath.Join(home, "kubeaid-cli", "old-cluster", "configs"),
			picked:      "old-cluster",
			final:       "demo-02",
			wantDirFunc: func() string { return filepath.Join(home, "kubeaid-cli", "demo-02", "configs") },
			wantCluster: "demo-02",
		},
		{
			name:        "an unnamed cluster falls back to where it started",
			directory:   filepath.Join(home, "kubeaid-cli", "demo-01", "configs"),
			picked:      "demo-01",
			final:       "",
			wantDirFunc: func() string { return filepath.Join(home, "kubeaid-cli", "demo-01", "configs") },
			wantCluster: "demo-01",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveWriteTarget(tc.directory, tc.picked, tc.final)
			require.NoError(t, err)

			assert.Equal(t, tc.wantDirFunc(), got.ConfigsDirectory)
			assert.Equal(t, tc.wantCluster, got.ClusterName)
		})
	}
}

// The whole point of moving the write: the config the operator chose to leave
// alone has to still be there afterwards.
func TestStartFreshUnderAnotherClusterDoesNotClobberIt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)

	oldDirectory := filepath.Join(home, "kubeaid-cli", "old-cluster", "configs")
	require.NoError(t, os.MkdirAll(oldDirectory, 0o700))
	oldGeneral := filepath.Join(oldDirectory, "general.yaml")
	require.NoError(t, os.WriteFile(oldGeneral, []byte("cluster:\n  name: old-cluster\n"), 0o600))

	target, err := resolveWriteTarget(oldDirectory, "old-cluster", "demo-02")
	require.NoError(t, err)
	require.NotEqual(t, oldDirectory, target.ConfigsDirectory)

	// Simulate the write landing at the resolved target.
	require.NoError(t, os.MkdirAll(target.ConfigsDirectory, 0o700))
	require.NoError(t, os.WriteFile(
		filepath.Join(target.ConfigsDirectory, "general.yaml"),
		[]byte("cluster:\n  name: demo-02\n"), 0o600))

	survived, err := os.ReadFile(oldGeneral)
	require.NoError(t, err)
	assert.Contains(t, string(survived), "old-cluster")
}
