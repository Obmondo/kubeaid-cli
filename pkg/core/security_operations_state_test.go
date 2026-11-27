// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/kubernetes"
)

// renderedState reads every sealed file and the hashes a render left in dir.
func renderedState(t *testing.T, dir string) (map[string]string, config.SecurityOperationsWazuhCredentials,
	map[string]config.SecurityOperationsWazuhCredentials,
) {
	t.Helper()

	sealed := map[string]string{}
	require.NoError(t, filepath.WalkDir(filepath.Join(dir, "sealed-secrets"),
		func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			raw, err := os.ReadFile(p) //nolint:gosec // G122: the test's own temp dir.
			rel, _ := filepath.Rel(dir, p)
			sealed[rel] = string(raw)
			return err
		}))
	central, tenants, err := readRenderedCredentialState(dir)
	require.NoError(t, err)
	return sealed, central, tenants
}

// TestRenderSecurityOperationsKeepsStateInTheClusterDir renders without any
// secrets.yaml credentials: the first run generates passwords, later runs keep
// every existing release untouched, a new tenant gets fresh credentials, and a
// tenant whose sealed files are gone gets new ones without touching the rest.
func TestRenderSecurityOperationsKeepsStateInTheClusterDir(t *testing.T) {
	withSIEMConfig(t, siemTenant(1, "Tenant A"), siemTenant(2, "Tenant B"))
	config.ParsedSecretsConfig = &config.SecretsConfig{}

	orig := kubernetes.SealingCertSource
	kubernetes.SealingCertSource = writeTestSealingCert(t)
	t.Cleanup(func() { kubernetes.SealingCertSource = orig })

	ctx := context.Background()
	dir := t.TempDir()

	_, err := RenderSecurityOperations(ctx, dir)
	require.NoError(t, err)
	sealed1, central1, tenants1 := renderedState(t, dir)
	require.Len(t, sealed1, 2+2*4)
	assert.NotEmpty(t, central1.IndexerPasswordHash)
	assert.NotEmpty(t, tenants1["001"].ClusterKey)
	assert.NotEqual(t, tenants1["001"].IndexerPasswordHash, tenants1["002"].IndexerPasswordHash)

	// Second run: nothing changes.
	_, err = RenderSecurityOperations(ctx, dir)
	require.NoError(t, err)
	sealed2, central2, tenants2 := renderedState(t, dir)
	assert.Equal(t, sealed1, sealed2)
	assert.Equal(t, central1, central2)
	assert.Equal(t, tenants1, tenants2)

	// A third tenant: 001, 002 and the central search stay as they were.
	config.ParsedGeneralConfig.Cluster.SecurityOperations.Tenants = append(
		config.ParsedGeneralConfig.Cluster.SecurityOperations.Tenants, siemTenant(3, "Tenant C"))
	_, err = RenderSecurityOperations(ctx, dir)
	require.NoError(t, err)
	sealed3, central3, tenants3 := renderedState(t, dir)
	for path, content := range sealed1 {
		assert.Equal(t, content, sealed3[path], "%s unchanged", path)
	}
	assert.Equal(t, central1, central3)
	assert.Equal(t, tenants1["001"], tenants3["001"])
	assert.Equal(t, tenants1["002"], tenants3["002"])
	assert.NotEmpty(t, tenants3["003"].IndexerPasswordHash)
	assert.Contains(t, sealed3, "sealed-secrets/wazuh-003/wazuh-authd-pass.yaml")

	// Tenant 002 lost a sealed file: only 002 gets new credentials.
	require.NoError(t, os.Remove(filepath.Join(dir, "sealed-secrets/wazuh-002/wazuh-api-cred.yaml")))
	_, err = RenderSecurityOperations(ctx, dir)
	require.NoError(t, err)
	sealed4, central4, tenants4 := renderedState(t, dir)
	assert.Equal(t, central1, central4)
	assert.Equal(t, tenants3["001"], tenants4["001"])
	assert.Equal(t, tenants3["003"], tenants4["003"])
	assert.NotEqual(t, tenants3["002"].IndexerPasswordHash, tenants4["002"].IndexerPasswordHash)
	assert.Contains(t, sealed4, "sealed-secrets/wazuh-002/wazuh-api-cred.yaml")
	assert.Equal(t, sealed3["sealed-secrets/wazuh-001/wazuh-api-cred.yaml"],
		sealed4["sealed-secrets/wazuh-001/wazuh-api-cred.yaml"])
}
