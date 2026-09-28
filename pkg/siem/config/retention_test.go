// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const retentionBase = `{
	"domain": "example.com",
	"keycloak": {"url": "https://kc", "realm": "soc", "adminSecretRef": {"namespace": "k", "name": "a", "key": "p"}},
	"operators": {},
	"tenants": [{"code": "001", "name": "A", "retentionDays": %s}],
	"components": {"wazuh": [{"tenant": "001", "url": "https://w", "credSecretRef": {"namespace": "n", "name": "c"}, "indexer": %s}]},
	"retention": %s
}`

func TestRetentionDefaults(t *testing.T) {
	t.Parallel()
	cfg, err := Parse(fmtJSON(retentionBase, "30", `{"url": "https://i", "credSecretRef": {"namespace": "n", "name": "ic"}}`, `{}`))
	require.NoError(t, err)
	require.NotNil(t, cfg.Retention)
	assert.Equal(t, DefaultRetentionPolicyID, cfg.Retention.PolicyID)
	assert.Equal(t, DefaultRetentionIndexPatterns, cfg.Retention.IndexPatterns)
	assert.Equal(t, DefaultCentralRetentionIndexPatterns, cfg.Retention.CentralIndexPatterns)
	ix := cfg.Components.Wazuh[0].Indexer
	assert.Equal(t, DefaultIndexerUserKey, ix.CredSecretRef.UsernameKey)
	assert.Equal(t, DefaultIndexerPassKey, ix.CredSecretRef.PasswordKey)
}

func TestRetentionValidation(t *testing.T) {
	t.Parallel()
	_, err := Parse(fmtJSON(retentionBase, "4000", `{"url": "", "credSecretRef": {}}`,
		`{"policyId": "Bad ID", "warmAfterDays": -1, "indexPatterns": ["*", "a,b", "wazuh-alerts-*"]}`))
	require.Error(t, err)
	for _, want := range []string{
		"tenants[0].retentionDays must not exceed 3650",
		"components.wazuh[0].indexer.url is required",
		"components.wazuh[0].indexer.credSecretRef needs namespace and name",
		`retention.policyId "Bad ID"`,
		"retention.warmAfterDays and retention.centralDays must be in 0-3650",
		`retention.indexPatterns[0] "*"`,
		`retention.indexPatterns[1] "a,b"`,
	} {
		assert.Contains(t, err.Error(), want)
	}
	assert.NotContains(t, err.Error(), "indexPatterns[2]")
}

func fmtJSON(format string, args ...any) []byte {
	return []byte(fmt.Sprintf(format, args...))
}
