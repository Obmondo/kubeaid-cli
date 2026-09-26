// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package parser

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
	"gopkg.in/yaml.v3"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/globals"
)

// withSecretsFile points globals.ConfigsDirectory at a temp dir holding
// secrets.yaml with contents, and resets ParsedSecretsConfig.
func withSecretsFile(t *testing.T, contents string) string {
	t.Helper()

	dir := t.TempDir()
	secretsPath := filepath.Join(dir, "secrets.yaml")
	require.NoError(t, os.WriteFile(secretsPath, []byte(contents), 0o600))

	origDir, origSecrets := globals.ConfigsDirectory, config.ParsedSecretsConfig
	globals.ConfigsDirectory = dir
	config.ParsedSecretsConfig = &config.SecretsConfig{}
	t.Cleanup(func() {
		globals.ConfigsDirectory = origDir
		config.ParsedSecretsConfig = origSecrets
	})
	return secretsPath
}

func readSOCSecrets(t *testing.T, secretsPath string) *config.SecurityOperationsCredentials {
	t.Helper()

	raw, err := os.ReadFile(secretsPath)
	require.NoError(t, err)
	var parsed config.SecretsConfig
	require.NoError(t, yaml.Unmarshal(raw, &parsed))
	require.NotNil(t, parsed.SecurityOperations)
	return parsed.SecurityOperations
}

// wazuhAPIPolicy is the Wazuh API password policy: 8-64 characters with an
// upper and a lower case letter, a digit and a symbol.
func assertWazuhAPIPolicy(t *testing.T, password string) {
	t.Helper()

	assert.GreaterOrEqual(t, len(password), 8)
	assert.LessOrEqual(t, len(password), 64)
	for _, class := range []string{`[A-Z]`, `[a-z]`, `[0-9]`, `[^A-Za-z0-9]`} {
		assert.Regexp(t, regexp.MustCompile(class), password, "API password lacks %s", class)
	}
	assert.Regexp(t, regexp.MustCompile(`^[A-Za-z0-9.]+$`), password, "shell and YAML safe")
}

func TestFillSecurityOperationsSecrets(t *testing.T) {
	ctx := context.Background()

	withSecurityOperations(t, validSOC(
		config.SecurityOperationsTenant{Code: "001", Name: testSOCTenantA},
		config.SecurityOperationsTenant{Code: "002", Name: testSOCTenantB},
	), nil)

	secretsPath := withSecretsFile(t, "# operator comment\nhetzner:\n  apiToken: keep-me\n")

	require.NoError(t, FillMissingSecrets(ctx))
	first := readSOCSecrets(t, secretsPath)

	t.Run("passwords and hashes generated", func(t *testing.T) {
		for _, creds := range []config.SecurityOperationsWazuhCredentials{
			first.Central, first.Tenants["001"], first.Tenants["002"],
		} {
			assert.Len(t, creds.IndexerPassword, 32)
			assert.Len(t, creds.DashboardPassword, 32)
			require.NoError(t, bcrypt.CompareHashAndPassword(
				[]byte(creds.IndexerPasswordHash), []byte(creds.IndexerPassword)))
			require.NoError(t, bcrypt.CompareHashAndPassword(
				[]byte(creds.DashboardPasswordHash), []byte(creds.DashboardPassword)))
			cost, err := bcrypt.Cost([]byte(creds.IndexerPasswordHash))
			require.NoError(t, err)
			assert.Equal(t, 12, cost)
		}
		assert.Empty(t, first.Central.APIPassword, "the central search runs no manager")
		assert.NotEqual(t, first.Tenants["001"].IndexerPassword, first.Tenants["002"].IndexerPassword)
	})

	t.Run("tenant API, authd and cluster key", func(t *testing.T) {
		for _, code := range []string{"001", "002"} {
			creds := first.Tenants[code]
			assertWazuhAPIPolicy(t, creds.APIPassword)
			assert.Len(t, creds.AuthdPassword, 32)
			assert.Len(t, creds.ClusterKey, 32)
		}
	})

	t.Run("operator content survives and codes stay strings", func(t *testing.T) {
		raw, err := os.ReadFile(secretsPath)
		require.NoError(t, err)
		assert.Contains(t, string(raw), "# operator comment")
		assert.Contains(t, string(raw), "apiToken: keep-me")
		assert.Contains(t, string(raw), `"001":`)
	})

	t.Run("in-memory config refreshed", func(t *testing.T) {
		require.NotNil(t, config.ParsedSecretsConfig.SecurityOperations)
		assert.Equal(t, first.Tenants["001"].APIPassword,
			config.ParsedSecretsConfig.SecurityOperations.Tenants["001"].APIPassword)
	})

	t.Run("second run changes nothing", func(t *testing.T) {
		before, err := os.ReadFile(secretsPath)
		require.NoError(t, err)

		require.NoError(t, FillMissingSecrets(ctx))

		after, err := os.ReadFile(secretsPath)
		require.NoError(t, err)
		assert.Equal(t, string(before), string(after), "hashes must be stable across runs")
	})

	t.Run("hash follows a password change", func(t *testing.T) {
		raw, err := os.ReadFile(secretsPath)
		require.NoError(t, err)
		var root yaml.Node
		require.NoError(t, yaml.Unmarshal(raw, &root))
		docMap, err := documentRootMapping(&root)
		require.NoError(t, err)
		soc, err := ensureMappingChild(docMap, "securityOperations")
		require.NoError(t, err)
		tenants, err := ensureMappingChild(soc, "tenants")
		require.NoError(t, err)
		tenant, err := ensureMappingChild(tenants, "002")
		require.NoError(t, err)
		setScalar(tenant, "dashboardPassword", "ChangedByTheOperator123")
		out, err := yaml.Marshal(&root)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(secretsPath, out, 0o600))

		require.NoError(t, FillMissingSecrets(ctx))
		after := readSOCSecrets(t, secretsPath)

		changed := after.Tenants["002"]
		assert.Equal(t, "ChangedByTheOperator123", changed.DashboardPassword)
		assert.NotEqual(t, first.Tenants["002"].DashboardPasswordHash, changed.DashboardPasswordHash)
		require.NoError(t, bcrypt.CompareHashAndPassword(
			[]byte(changed.DashboardPasswordHash), []byte("ChangedByTheOperator123")))

		assert.Equal(t, first.Tenants["002"].IndexerPasswordHash, changed.IndexerPasswordHash,
			"an unchanged password keeps its hash")
		assert.Equal(t, first.Tenants["001"], after.Tenants["001"])
	})

	t.Run("a new tenant adds only its own entry", func(t *testing.T) {
		before := readSOCSecrets(t, secretsPath)
		config.ParsedGeneralConfig.Cluster.SecurityOperations.Tenants = append(
			config.ParsedGeneralConfig.Cluster.SecurityOperations.Tenants,
			config.SecurityOperationsTenant{Code: "003", Name: "Tenant C"},
		)

		require.NoError(t, FillMissingSecrets(ctx))
		after := readSOCSecrets(t, secretsPath)

		assert.Equal(t, before.Central, after.Central)
		assert.Equal(t, before.Tenants["001"], after.Tenants["001"])
		assert.Equal(t, before.Tenants["002"], after.Tenants["002"])
		assert.NotEmpty(t, after.Tenants["003"].APIPassword)
	})
}

func TestFillSecurityOperationsSecretsDisabled(t *testing.T) {
	cfg := validSOC(config.SecurityOperationsTenant{Code: "001", Name: testSOCTenantA})
	cfg.Enabled = false
	withSecurityOperations(t, cfg, nil)
	secretsPath := withSecretsFile(t, "hetzner:\n  apiToken: keep-me\n")

	require.NoError(t, FillMissingSecrets(context.Background()))

	raw, err := os.ReadFile(secretsPath)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "securityOperations")
}

func TestWazuhAPIPassword(t *testing.T) {
	for range 20 {
		password, err := wazuhAPIPassword()
		require.NoError(t, err)
		assert.Len(t, password, 32)
		assertWazuhAPIPolicy(t, password)
	}
}
