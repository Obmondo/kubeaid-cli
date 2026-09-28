// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package siemctl

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
)

const generalWithComments = `# Top comment stays.
forkURLs:
  kubeaid:
    url: https://github.com/example/KubeAid # the fork
  kubeaidConfig:
    url: https://git.example.com/example/kubeaid-config.git
cluster:
  name: demo
  # The SOC.
  securityOperations:
    enabled: true
    domain: old.example.com
    reconciler:
      enabled: true
      dryRun: false
    tenants:
      - {code: "001", name: Tenant A}
`

func TestGeneralConfigRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "general.yaml")
	require.NoError(t, os.WriteFile(path, []byte(generalWithComments), 0o600))

	f, err := OpenGeneralConfig(path)
	require.NoError(t, err)
	cfg, err := f.SecurityOperations()
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, "old.example.com", cfg.Domain)
	require.NotNil(t, cfg.Reconciler.DryRun)
	assert.False(t, *cfg.Reconciler.DryRun)

	cfg.Domain = "soc.example.com"
	require.NoError(t, AddTenant(cfg, config.SecurityOperationsTenant{Code: "002", Name: "Tenant B"}))
	require.NoError(t, f.SetSecurityOperations(cfg))
	require.NoError(t, f.Save())

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	out := string(raw)
	assert.Contains(t, out, "# Top comment stays.")
	assert.Contains(t, out, "# the fork")
	assert.Contains(t, out, "# The SOC.")
	assert.Contains(t, out, "domain: soc.example.com")
	// dryRun: false differs from absent (nil means true), so it stays.
	assert.Contains(t, out, "dryRun: false")
	// Zero values are not written.
	assert.NotContains(t, out, "chartRevision")
	assert.NotContains(t, out, "null")
	assert.NotContains(t, out, `""`)

	f2, err := OpenGeneralConfig(path)
	require.NoError(t, err)
	cfg2, err := f2.SecurityOperations()
	require.NoError(t, err)
	require.Len(t, cfg2.Tenants, 2)
	assert.Equal(t, "Tenant B", cfg2.Tenants[1].Name)
}

func TestGeneralConfigNewFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "general.yaml")
	f, err := OpenGeneralConfig(path)
	require.NoError(t, err)
	assert.False(t, f.Exists())
	f.SetString("demo", "cluster", "name")
	f.SetString("https://github.com/example/KubeAid", "forkURLs", "kubeaid", "url")
	require.NoError(t, f.SetSecurityOperations(&config.SecurityOperationsConfig{Enabled: true, Domain: "example.com"}))
	raw, err := f.Bytes()
	require.NoError(t, err)
	assert.Equal(t, "demo", f.GetString("cluster", "name"))
	assert.True(t, strings.Contains(string(raw), "securityOperations:\n    enabled: true\n    domain: example.com"), string(raw))
}

func TestAddRemoveTenant(t *testing.T) {
	cfg := &config.SecurityOperationsConfig{}
	require.NoError(t, AddTenant(cfg, config.SecurityOperationsTenant{Code: "001", Name: "A"}))
	assert.ErrorContains(t, AddTenant(cfg, config.SecurityOperationsTenant{Code: "001", Name: "B"}), "already exists")
	assert.ErrorContains(t, AddTenant(cfg, config.SecurityOperationsTenant{Code: "002", Name: "A"}), "already used")
	assert.ErrorContains(t, AddTenant(cfg, config.SecurityOperationsTenant{Code: "Bad!", Name: "C"}), "must match")
	assert.ErrorContains(t, AddTenant(cfg, config.SecurityOperationsTenant{Code: "003"}), "name is required")

	removed, err := RemoveTenant(cfg, "001")
	require.NoError(t, err)
	assert.Equal(t, "A", removed.Name)
	assert.Empty(t, cfg.Tenants)
	_, err = RemoveTenant(cfg, "001")
	assert.Error(t, err)
}
