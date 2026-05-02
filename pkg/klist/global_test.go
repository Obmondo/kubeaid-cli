// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package klist

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --------------------------------------------------------------------------
// LoadGlobal
// --------------------------------------------------------------------------

func TestLoadGlobal(t *testing.T) {
	t.Parallel()

	t.Run("missing file returns defaults without error", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()

		got, err := LoadGlobal(dir)
		require.NoError(t, err)
		assert.Equal(t, DefaultClusterPeerPrefix, got.NetBird.ClusterPeerPrefix)
		assert.Equal(t, DefaultClusterPeerSuffix, got.NetBird.ClusterPeerSuffix)
		assert.Empty(t, got.NetBird.ManagementURL)
	})

	t.Run("present file is parsed and defaults fill in empty fields", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		write(t, filepath.Join(dir, GlobalConfigFile), `
netbird:
  managementUrl: https://netbird.example.com
  clusterPeerSuffix: .netbird.selfhosted
`)

		got, err := LoadGlobal(dir)
		require.NoError(t, err)
		assert.Equal(t, "https://netbird.example.com", got.NetBird.ManagementURL)
		assert.Equal(t, ".netbird.selfhosted", got.NetBird.ClusterPeerSuffix)
		// Prefix wasn't set, so default kicks in.
		assert.Equal(t, DefaultClusterPeerPrefix, got.NetBird.ClusterPeerPrefix)
	})

	t.Run("malformed YAML returns parse error", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		write(t, filepath.Join(dir, GlobalConfigFile), "not: valid: yaml:\n  - item")

		_, err := LoadGlobal(dir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "parsing")
	})
}

// --------------------------------------------------------------------------
// ListClusters
// --------------------------------------------------------------------------

func TestListClusters(t *testing.T) {
	t.Parallel()

	t.Run("walks customer directories and sorts (customer, cluster)", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		mkdir(t, dir, "clusters/acme")
		mkdir(t, dir, "clusters/bigcorp")
		mkdir(t, dir, "clusters/zeta")
		write(t, filepath.Join(dir, "clusters/acme/staging.yaml"), "name: staging")
		write(t, filepath.Join(dir, "clusters/acme/prod.yaml"), "name: prod")
		// Customer-defaults file: must be skipped.
		write(t, filepath.Join(dir, "clusters/acme/_customer.yaml"), "customer: acme")
		write(t, filepath.Join(dir, "clusters/bigcorp/staging.yaml"), "name: staging")
		write(t, filepath.Join(dir, "clusters/zeta/dev.yaml"), "name: dev")
		// Non-yaml file: must be skipped.
		write(t, filepath.Join(dir, "clusters/zeta/README.md"), "# notes")

		refs, err := ListClusters(dir)
		require.NoError(t, err)

		want := []ClusterRef{
			{Customer: "acme", ClusterName: "prod", YAMLPath: filepath.Join(dir, "clusters/acme/prod.yaml")},
			{Customer: "acme", ClusterName: "staging", YAMLPath: filepath.Join(dir, "clusters/acme/staging.yaml")},
			{Customer: "bigcorp", ClusterName: "staging", YAMLPath: filepath.Join(dir, "clusters/bigcorp/staging.yaml")},
			{Customer: "zeta", ClusterName: "dev", YAMLPath: filepath.Join(dir, "clusters/zeta/dev.yaml")},
		}
		assert.Equal(t, want, refs)
	})

	t.Run("missing clusters directory returns wrapped error", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()

		_, err := ListClusters(dir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "clusters")
	})

	t.Run("empty clusters directory returns nil slice and no error", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		mkdir(t, dir, "clusters")

		refs, err := ListClusters(dir)
		require.NoError(t, err)
		assert.Empty(t, refs)
	})
}

// --------------------------------------------------------------------------
// helpers (test-local)
// --------------------------------------------------------------------------

func mkdir(t *testing.T, base, rel string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(base, rel), 0o750))
}

func write(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}
