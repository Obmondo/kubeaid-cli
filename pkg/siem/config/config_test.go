// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadExample(t *testing.T) {
	t.Parallel()
	cfg, err := Load("testdata/tenants.example.json")
	require.NoError(t, err)

	assert.Equal(t, "soc", cfg.Keycloak.Realm)
	require.Len(t, cfg.Tenants, 2)
	assert.Equal(t, "tenant-001", cfg.GroupName(cfg.Tenants[0]))
	require.NotNil(t, cfg.Tenants[1].IdP)
	assert.Equal(t, "tenant-002-idp", cfg.Tenants[1].IdP.Alias, "alias defaults from the group name")
	assert.Equal(t, "Tenant B", cfg.Tenants[1].IdP.DisplayName)
	assert.Equal(t, GeneratorPassword, cfg.Secrets[1].Keys[0].Generator)
	assert.Len(t, cfg.Clients, 5)
	assert.Equal(t, "IrisInitialClient", cfg.Components.IRIS.InitialCustomer)
	assert.Equal(t, "readonly", cfg.Components.Wazuh.AnalystAPIRole)
}

func TestDefaults(t *testing.T) {
	t.Parallel()
	cfg, err := Parse([]byte(`{
		"domain": "example.com",
		"keycloak": {"url": "http://kc", "realm": "r", "adminSecretRef": {"namespace": "n", "name": "s", "key": "k"}, "mfaFlow": {}},
		"operators": {},
		"tenants": [{"code": "a1", "name": "A"}],
		"components": {
			"wazuh": {"url": "https://w", "credSecretRef": {"namespace": "n", "name": "c"}},
			"velociraptor": {"apiClientSecretRef": {"namespace": "n", "name": "v"}}
		}
	}`))
	require.NoError(t, err)
	assert.Equal(t, DefaultTenantGroupPrefix, cfg.TenantGroupPrefix)
	assert.Equal(t, "admin", cfg.Keycloak.AdminUsername)
	assert.Equal(t, DefaultMFAFlowAlias, cfg.Keycloak.MFAFlow.Alias)
	assert.Equal(t, DefaultMFAFlowCopyFrom, cfg.Keycloak.MFAFlow.CopyFrom)
	assert.Equal(t, DefaultWazuhUsernameKey, cfg.Components.Wazuh.CredSecretRef.UsernameKey)
	assert.Equal(t, DefaultWazuhAdminRole, cfg.Components.Wazuh.AdminAPIRole)
	assert.Equal(t, DefaultVeloAPIClientKey, cfg.Components.Velociraptor.APIClientSecretRef.Key)
}

func TestValidateErrors(t *testing.T) {
	t.Parallel()
	_, err := Parse([]byte(`{
		"domain": "",
		"tenantGroupPrefix": "Bad_",
		"keycloak": {"url": "", "realm": "", "adminSecretRef": {}},
		"operators": {},
		"tenants": [
			{"code": "A!", "name": ""},
			{"code": "b", "name": "dup", "retentionDays": -1},
			{"code": "b", "name": "dup"}
		],
		"clients": [
			{"clientId": "c", "publicClient": true, "serviceAccountsEnabled": true},
			{"clientId": "c", "serviceAccountClientRoles": {"x": ["y"]}, "protocolMappers": [{"name": ""}]}
		],
		"secrets": [{"namespace": "", "name": "", "keys": [{"key": "", "generator": "nope"}]}],
		"components": {
			"iris": {"url": "", "apiKeySecretRef": {}, "serviceAccounts": [{"login": ""}]},
			"wazuh": {"url": "", "credSecretRef": {}},
			"velociraptor": {"serverMonitoring": [{"artifact": ""}]}
		}
	}`))
	require.Error(t, err)
	msg := err.Error()
	for _, want := range []string{
		"domain is required",
		"tenantGroupPrefix",
		"keycloak.url and keycloak.realm",
		"keycloak.adminSecretRef",
		`tenants[0].code "A!"`,
		"tenants[0].name is required",
		`tenants[2].code "b" is not unique`,
		`tenants[2].name "dup" is not unique`,
		"tenants[1].retentionDays",
		"a public client has no secret",
		`clients[1].clientId "c" is not unique`,
		"serviceAccountClientRoles needs serviceAccountsEnabled",
		"protocolMappers[0] needs name",
		"secrets[0] needs namespace",
		`generator "nope"`,
		"secrets[0].keys[0].key is required",
		"components.iris.url",
		"components.iris.apiKeySecretRef",
		"serviceAccounts[0].login",
		"components.wazuh.url",
		"components.wazuh.credSecretRef",
		"exactly one of apiClientSecretRef and apiClientFile",
		"serverMonitoring[0].artifact",
	} {
		assert.Contains(t, msg, want)
	}
}

func TestUnknownFieldRejected(t *testing.T) {
	t.Parallel()
	_, err := Parse([]byte(`{"domain": "x", "tenantz": []}`))
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "unknown field"))
}

func TestLoadMissingFile(t *testing.T) {
	t.Parallel()
	_, err := Load("testdata/does-not-exist.json")
	require.Error(t, err)
}
