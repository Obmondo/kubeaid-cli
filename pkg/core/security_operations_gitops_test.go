// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/kubernetes"
)

// kubesealOutput is the shape kubeaid-cli writes: hash header, then kubeseal's YAML.
const kubesealOutput = `# kubeaid-sha256: 0123abcd
---
apiVersion: bitnami.com/v1alpha1
kind: SealedSecret
metadata:
  name: wazuh-api-cred
  namespace: wazuh-001
spec:
  encryptedData:
    API_PASSWORD: AgBciphertext
  template:
    metadata:
      labels:
        kubeaid.io/managed-by: kubeaid
      name: wazuh-api-cred
      namespace: wazuh-001
`

func TestAnnotateSealedSecret(t *testing.T) {
	annotated, err := annotateSealedSecret([]byte(kubesealOutput))
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(string(annotated), "# kubeaid-sha256: 0123abcd\n"), "hash header kept")

	var doc map[string]any
	require.NoError(t, yaml.Unmarshal(annotated, &doc))
	assert.Equal(t, map[string]any{
		"argocd.argoproj.io/sync-options": "Prune=false",
		"argocd.argoproj.io/sync-wave":    "-1",
	}, dig(t, doc, "metadata", "annotations"))
	assert.Equal(t, "AgBciphertext", dig(t, doc, "spec", "encryptedData", "API_PASSWORD"))
	assert.NotContains(t, digMap(t, doc, "spec", "template", "metadata"), "annotations",
		"only the SealedSecret is annotated, not the Secret it creates")

	again, err := annotateSealedSecret(annotated)
	require.NoError(t, err)
	assert.Equal(t, string(annotated), string(again), "idempotent")

	// An existing annotations block (e.g. a scope annotation) is extended.
	scoped := strings.Replace(kubesealOutput, "metadata:\n  name:",
		"metadata:\n  annotations:\n    sealedsecrets.bitnami.com/namespace-wide: \"true\"\n  name:", 1)
	annotated, err = annotateSealedSecret([]byte(scoped))
	require.NoError(t, err)
	require.NoError(t, yaml.Unmarshal(annotated, &doc))
	annotations := digMap(t, doc, "metadata", "annotations")
	assert.Len(t, annotations, 3)
	assert.Equal(t, "Prune=false", annotations["argocd.argoproj.io/sync-options"])

	_, err = annotateSealedSecret([]byte("kind: SealedSecret\n"))
	assert.Error(t, err)
}

// withTestSealingCert seals with a throwaway certificate for the test.
func withTestSealingCert(t *testing.T) {
	t.Helper()
	orig := kubernetes.SealingCertSource
	kubernetes.SealingCertSource = writeTestSealingCert(t)
	t.Cleanup(func() { kubernetes.SealingCertSource = orig })
}

// toLegacyLayout moves every owned sealed file of a render back to the
// sealed-secrets/<namespace>/ layout of older kubeaid-cli versions, without
// the Argo CD annotations those versions did not write.
func toLegacyLayout(t *testing.T, dir string) map[string]string {
	t.Helper()
	legacy := map[string]string{}
	for _, secret := range securityOperationsSecretFiles() {
		ns, name := secret.Data.Namespace, secret.Data.Name
		raw, err := os.ReadFile(filepath.Join(dir, securityOperationsSealedSecretPath(ns, name)))
		require.NoError(t, err)
		plain := string(raw)
		for _, kv := range securityOperationsSealedSecretAnnotations {
			plain = strings.Replace(plain, "    "+kv[0]+": "+kv[1]+"\n", "", 1)
		}
		plain = strings.Replace(plain, "  annotations:\n", "", 1)
		p := securityOperationsLegacySealedSecretPath(ns, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(dir, p)), 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(dir, p), []byte(plain), 0o600)) //nolint:gosec // G703: the test's temp dir.
		legacy[p] = plain
	}
	require.NoError(t, os.RemoveAll(filepath.Join(dir, SecurityOperationsSealedSecretsDir)))
	return legacy
}

// TestSecurityOperationsSecretMigration: a cluster rendered with the legacy
// layout is migrated without rotating anything: step one copies each sealed
// file unchanged to the owned directory and annotates both copies, step two
// removes the legacy copies.
func TestSecurityOperationsSecretMigration(t *testing.T) {
	withSIEMConfig(t, siemTenant(1, "Tenant A"), siemTenant(2, "Tenant B"))
	config.ParsedSecretsConfig = &config.SecretsConfig{}
	withTestSealingCert(t)

	ctx := context.Background()
	dir := t.TempDir()
	_, err := RenderSecurityOperations(ctx, dir)
	require.NoError(t, err)
	stateBefore, err := readRenderedCredentialState(dir)
	require.NoError(t, err)

	legacy := toLegacyLayout(t, dir)
	// Hand-sealed Secrets of the namespace stay with the shared secrets app.
	handSealed := filepath.Join(dir, "sealed-secrets/security-operations/oidc-credentials.yaml")
	require.NoError(t, os.WriteFile(handSealed, []byte("kind: SealedSecret\n"), 0o600))

	// Step one.
	written, err := RenderSecurityOperations(ctx, dir)
	require.NoError(t, err)
	stateAfter, err := readRenderedCredentialState(dir)
	require.NoError(t, err)
	assert.Equal(t, stateBefore, stateAfter, "no credential changes")

	for _, secret := range securityOperationsSecretFiles() {
		ns, name := secret.Data.Namespace, secret.Data.Name
		legacyPath := securityOperationsLegacySealedSecretPath(ns, name)
		ownedPath := securityOperationsSealedSecretPath(ns, name)
		assert.Contains(t, written, ownedPath)
		assert.Contains(t, written, legacyPath)

		owned, err := os.ReadFile(filepath.Join(dir, ownedPath))
		require.NoError(t, err)
		legacyNow, err := os.ReadFile(filepath.Join(dir, legacyPath))
		require.NoError(t, err)
		assert.Equal(t, string(owned), string(legacyNow), "both apps apply the same SealedSecret")
		reannotated, err := annotateSealedSecret([]byte(legacy[legacyPath]))
		require.NoError(t, err)
		assert.Equal(t, string(reannotated), string(owned), "same ciphertext, only annotated")
	}
	assert.Len(t, SecurityOperationsLegacySealedSecretFiles(dir), 2+2*5)

	// A rotation during the migration rewrites both copies.
	_, err = RenderSecurityOperationsWithOptions(ctx, dir, SecurityOperationsRenderOptions{Rotate: []string{"wazuh-001"}})
	require.NoError(t, err)
	owned, err := os.ReadFile(filepath.Join(dir, securityOperationsSealedSecretPath("wazuh-001", "wazuh-api-cred")))
	require.NoError(t, err)
	legacyNow, err := os.ReadFile(filepath.Join(dir, securityOperationsLegacySealedSecretPath("wazuh-001", "wazuh-api-cred")))
	require.NoError(t, err)
	assert.Equal(t, string(owned), string(legacyNow))
	assert.NotEqual(t, legacy[securityOperationsLegacySealedSecretPath("wazuh-001", "wazuh-api-cred")], string(legacyNow))

	// Step two.
	removed, err := RemoveSecurityOperationsLegacySealedSecrets(dir)
	require.NoError(t, err)
	assert.Len(t, removed, 2+2*5)
	assert.Empty(t, SecurityOperationsLegacySealedSecretFiles(dir))
	assert.NoDirExists(t, filepath.Join(dir, "sealed-secrets/wazuh-001"))
	assert.FileExists(t, handSealed, "only kubeaid-cli's files are removed")

	_, err = RenderSecurityOperations(ctx, dir)
	require.NoError(t, err)
	assert.Empty(t, SecurityOperationsLegacySealedSecretFiles(dir), "legacy files are never written again")
}

func TestRemoveLegacySealedSecretsNeedsTheOwnedCopy(t *testing.T) {
	withSIEMConfig(t, siemTenant(1, "Tenant A"))
	dir := t.TempDir()
	p := filepath.Join(dir, securityOperationsLegacySealedSecretPath("wazuh-001", "wazuh-api-cred"))
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o750))
	require.NoError(t, os.WriteFile(p, []byte(kubesealOutput), 0o600))

	_, err := RemoveSecurityOperationsLegacySealedSecrets(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "render first")
	assert.FileExists(t, p)
}

// TestRenderSecurityOperationsRefusesSilentRotation: an existing release
// whose sealed files are all gone, or whose values lost a hash, is an error;
// --rotate names releases or all of them, and rejects unknown ones.
func TestRenderSecurityOperationsRefusesSilentRotation(t *testing.T) {
	withSIEMConfig(t, siemTenant(1, "Tenant A"))
	config.ParsedSecretsConfig = &config.SecretsConfig{}
	withTestSealingCert(t)

	ctx := context.Background()
	dir := t.TempDir()
	_, err := RenderSecurityOperations(ctx, dir)
	require.NoError(t, err)
	clusterKeyPath := filepath.Join(dir,
		securityOperationsSealedSecretPath("wazuh-001", "wazuh-manager-cluster-key"))
	firstKey, err := os.ReadFile(clusterKeyPath)
	require.NoError(t, err)

	// Every sealed file of wazuh-001 gone: its Application still exists.
	require.NoError(t, os.RemoveAll(filepath.Join(dir, SecurityOperationsSealedSecretsDir, "wazuh-001")))
	_, err = RenderSecurityOperations(ctx, dir)
	var incomplete *IncompleteSecurityOperationsStateError
	require.ErrorAs(t, err, &incomplete)
	assert.Contains(t, err.Error(), "wazuh-001")
	assert.Contains(t, err.Error(), "wazuh-authd-pass.yaml")

	// The central values lost a hash.
	valuesPath := filepath.Join(dir, securityOperationsCentralValuesFile)
	raw, err := os.ReadFile(valuesPath)
	require.NoError(t, err)
	state, err := readRenderedCredentialState(dir)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(valuesPath, //nolint:gosec // G703: the test's temp dir.
		[]byte(strings.Replace(string(raw), state.central.IndexerPasswordHash, "", 1)), 0o600))
	_, err = RenderSecurityOperations(ctx, dir)
	require.ErrorAs(t, err, &incomplete)
	assert.Len(t, incomplete.Releases, 2)
	assert.Contains(t, err.Error(), "indexer password hash in "+securityOperationsCentralValuesFile)

	_, err = RenderSecurityOperationsWithOptions(ctx, dir, SecurityOperationsRenderOptions{Rotate: []string{"wazuh-999"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `cannot rotate "wazuh-999"`)

	_, err = RenderSecurityOperationsWithOptions(ctx, dir,
		SecurityOperationsRenderOptions{Rotate: []string{SecurityOperationsRotateAll}})
	require.NoError(t, err)
	after, err := readRenderedCredentialState(dir)
	require.NoError(t, err)
	assert.NotEmpty(t, after.central.IndexerPasswordHash)
	assert.NotEqual(t, state.central.DashboardPasswordHash, after.central.DashboardPasswordHash, "rotated")
	rotatedKey, err := os.ReadFile(clusterKeyPath)
	require.NoError(t, err)
	assert.NotEqual(t, string(firstKey), string(rotatedKey), "the sealed cluster key is rotated too")
}

func TestSIEMSyncPolicy(t *testing.T) {
	withSIEMConfig(t, siemTenant(1, "Tenant A"))
	sync := &config.ParsedGeneralConfig.Cluster.SecurityOperations.Sync

	render := func() map[string]map[string]any {
		tv := forkTV("")
		tv.SecOps = buildSecurityOperationsValues()
		return siemApps(t, tv)
	}

	sync.Automated = true
	for name, app := range render() {
		policy := digMap(t, app, "spec", "syncPolicy")
		assert.Equal(t, map[string]any{"selfHeal": true, "prune": false}, policy["automated"], name)
		assert.Contains(t, policy["syncOptions"], "ServerSideApply=true", name)
	}

	sync.Prune = true
	serverSideApply := false
	sync.ServerSideApply = &serverSideApply
	for name, app := range render() {
		policy := digMap(t, app, "spec", "syncPolicy")
		assert.Equal(t, map[string]any{"selfHeal": true, "prune": true}, policy["automated"], name)
		assert.NotContains(t, policy["syncOptions"], "ServerSideApply=true", name)
	}

	assert.Equal(t, []string{"security-operations", "wazuh-001"}, SecurityOperationsApplicationsInSyncOrder())
}
