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
	require.Len(t, cfg.Components.Wazuh, 2)
	assert.Equal(t, "readonly", cfg.Components.Wazuh[0].AnalystAPIRole)
	assert.Equal(t, DefaultWazuhUsernameKey, cfg.Components.Wazuh[1].CredSecretRef.UsernameKey)
	assert.Equal(t, DefaultWazuhTenantRole, cfg.Components.Wazuh[1].TenantAPIRole)
	require.NotNil(t, cfg.Components.WazuhCentral)
	assert.Equal(t, "t002", cfg.Components.WazuhCentral.Remotes[1].Alias)
	assert.Equal(t, "wazuh-app-config", cfg.Components.WazuhCentral.DashboardConfigSecret.Name)
	require.Len(t, cfg.Components.WazuhCentral.IndexPatterns, 2)
	assert.Equal(t, DefaultIndexPatternTimeField, cfg.Components.WazuhCentral.IndexPatterns[0].TimeFieldName)
	assert.True(t, cfg.Components.WazuhCentral.IndexPatterns[0].Default)
	require.Len(t, cfg.SecretCopies, 2)
	assert.Equal(t, "wazuh-002", cfg.SecretCopies[1].To.Namespace)
	require.Len(t, cfg.Enrolment, 2)
	assert.Equal(t, 21025, cfg.Enrolment[1].RegistrationPort)
	assert.Equal(t, DefaultWazuhAgentVersion, cfg.Enrolment[1].AgentVersion)
}

func TestDefaults(t *testing.T) {
	t.Parallel()
	cfg, err := Parse([]byte(`{
		"domain": "example.com",
		"keycloak": {"url": "http://kc", "realm": "r", "adminSecretRef": {"namespace": "n", "name": "s", "key": "k"}, "mfaFlow": {}},
		"operators": {},
		"tenants": [{"code": "a1", "name": "A"}],
		"components": {
			"wazuh": [{"tenant": "a1", "url": "https://w", "credSecretRef": {"namespace": "n", "name": "c"}}],
			"wazuhCentral": {"url": "https://i", "credSecretRef": {"namespace": "n", "name": "i"}},
			"velociraptor": {"apiClientSecretRef": {"namespace": "n", "name": "v"}}
		}
	}`))
	require.NoError(t, err)
	assert.Equal(t, DefaultTenantGroupPrefix, cfg.TenantGroupPrefix)
	assert.Equal(t, "admin", cfg.Keycloak.AdminUsername)
	assert.Equal(t, DefaultMFAFlowAlias, cfg.Keycloak.MFAFlow.Alias)
	assert.Equal(t, DefaultMFAFlowCopyFrom, cfg.Keycloak.MFAFlow.CopyFrom)
	w := cfg.Components.Wazuh[0]
	assert.Equal(t, DefaultWazuhUsernameKey, w.CredSecretRef.UsernameKey)
	assert.Equal(t, DefaultWazuhPasswordKey, w.CredSecretRef.PasswordKey)
	assert.Equal(t, DefaultWazuhAdminRole, w.AdminAPIRole)
	assert.Equal(t, DefaultWazuhAnalystRole, w.AnalystAPIRole)
	assert.Equal(t, DefaultWazuhTenantRole, w.TenantAPIRole)
	assert.Equal(t, DefaultIndexerUserKey, cfg.Components.WazuhCentral.CredSecretRef.UsernameKey)
	assert.Equal(t, DefaultIndexerPassKey, cfg.Components.WazuhCentral.CredSecretRef.PasswordKey)
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
		"secrets": [
			{"namespace": "", "name": "", "keys": [{"key": "", "generator": "nope"}]},
			{"namespace": "n", "name": "oidc", "keys": [{"key": "CLIENT_ID", "value": "c", "generator": "ignored"}]}
		],
		"secretCopies": [
			{"from": {}, "to": {"namespace": "n", "name": "s", "key": "k"}},
			{"from": {"namespace": "n", "name": "s", "key": "k"}, "to": {"namespace": "n", "name": "s", "key": "k"}},
			{"from": {"namespace": "m", "name": "s", "key": "k"}, "to": {"namespace": "n", "name": "s", "key": "k"}}
		],
		"components": {
			"iris": {"url": "", "apiKeySecretRef": {}, "serviceAccounts": [{"login": ""}]},
			"wazuh": [
				{"tenant": "b", "url": "", "credSecretRef": {}},
				{"tenant": "b", "url": "https://w", "credSecretRef": {"namespace": "n", "name": "c"}},
				{"tenant": "zz", "url": "https://w", "credSecretRef": {"namespace": "n", "name": "c"}}
			],
			"wazuhCentral": {
				"url": "", "credSecretRef": {}, "dashboardConfigSecret": {"namespace": "n"},
				"indexPatterns": [{"title": ""}, {"title": "a", "default": true}, {"title": "a", "default": true}],
				"remotes": [{"alias": "Bad.Alias", "seeds": []}, {"alias": "t1", "seeds": [""]}, {"alias": "t1", "seeds": ["h:9300"]}]
			},
			"velociraptor": {"serverMonitoring": [{"artifact": ""}]}
		},
		"enrolment": [
			{"tenant": "nope", "namespace": "", "name": "", "managerHost": "", "registrationPort": 0, "eventsPort": 70000, "authdSecretRef": {}, "agentVersion": "latest"},
			{"tenant": "b", "namespace": "n", "name": "e", "managerHost": "h", "registrationPort": 1515, "eventsPort": 1514, "authdSecretRef": {"namespace": "n", "name": "a", "key": "k"}},
			{"tenant": "b", "namespace": "n", "name": "e", "managerHost": "h", "registrationPort": 1515, "eventsPort": 1514, "authdSecretRef": {"namespace": "n", "name": "a", "key": "k"}}
		]
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
		"secretCopies[0].from needs namespace, name and key",
		"secretCopies[1] copies n/s/k onto itself",
		"secretCopies[2].to n/s/k is not unique",
		"components.iris.url",
		"components.iris.apiKeySecretRef",
		"serviceAccounts[0].login",
		"components.wazuh[0].url is required",
		"components.wazuh[0].credSecretRef",
		`components.wazuh[1].tenant "b" already has a manager`,
		`components.wazuh[2].tenant "zz" is not a tenant code`,
		"components.wazuhCentral.url is required",
		"components.wazuhCentral.credSecretRef",
		"components.wazuhCentral.dashboardConfigSecret needs namespace and name",
		"components.wazuhCentral.indexPatterns needs dashboardURL",
		"components.wazuhCentral.indexPatterns[0].title is required",
		`components.wazuhCentral.indexPatterns[2].title "a" is not unique`,
		"at most one pattern can be default, 2 are",
		`components.wazuhCentral.remotes[0].alias "Bad.Alias" must match`,
		"components.wazuhCentral.remotes[0].seeds must not be empty",
		"components.wazuhCentral.remotes[1].seeds[0] is empty",
		`components.wazuhCentral.remotes[2].alias "t1" is not unique`,
		`enrolment[0].tenant "nope" is not a tenant code`,
		"enrolment[0] needs namespace and name",
		"enrolment[0].managerHost is required",
		"enrolment[0].registrationPort 0 is not in 1-65535",
		"enrolment[0].eventsPort 70000 is not in 1-65535",
		"enrolment[0].authdSecretRef needs namespace, name and key",
		`enrolment[0].agentVersion "latest" must look like`,
		"enrolment[2]: Secret n/e is not unique",
		"exactly one of apiClientSecretRef and apiClientFile",
		"serverMonitoring[0].artifact",
	} {
		assert.Contains(t, msg, want)
	}
	assert.NotContains(t, msg, "secrets[1]", "a literal value needs no generator")
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

func TestOldSingleWazuhObjectRejected(t *testing.T) {
	t.Parallel()
	_, err := Parse([]byte(`{
		"domain": "example.com",
		"keycloak": {"url": "http://kc", "realm": "r", "adminSecretRef": {"namespace": "n", "name": "s", "key": "k"}},
		"operators": {},
		"tenants": [{"code": "001", "name": "A"}],
		"components": {"wazuh": {"url": "https://w", "credSecretRef": {"namespace": "n", "name": "c"}, "createGroups": true}}
	}`))
	require.Error(t, err, "components.wazuh is a list now")
}
