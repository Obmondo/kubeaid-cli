// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const identitiesBase = `{
	"domain": "example.com",
	"keycloak": {"url": "http://kc", "realm": "soc"%s},
	"operators": {},
	"tenants": [{"code": "a1", "name": "A"}],
	"components": {%s}
}`

func parseIdentities(t *testing.T, keycloak, components string) (*Config, error) {
	t.Helper()
	return Parse([]byte(fmt.Sprintf(identitiesBase, keycloak, components)))
}

func TestClientCredentialsWithoutAdmin(t *testing.T) {
	t.Parallel()
	cfg, err := parseIdentities(t,
		`, "clientCredentials": {"clientId": "siem-reconciler", "secretRef": {"namespace": "n", "name": "s", "key": "k"}}`, ``)
	require.NoError(t, err)
	assert.False(t, cfg.Keycloak.HasAdmin())
	assert.Equal(t, "siem-reconciler", cfg.Keycloak.ClientCredentials.ClientID)

	_, err = parseIdentities(t, `, "clientCredentials": {"clientId": "", "secretRef": {}}`, ``)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "keycloak.clientCredentials.clientId is required")
	assert.Contains(t, err.Error(), "keycloak.clientCredentials.secretRef needs")
	assert.NotContains(t, err.Error(), "adminSecretRef", "the admin is optional with client credentials")

	_, err = parseIdentities(t, ``, ``)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "keycloak.adminSecretRef needs")
}

func TestIRISGroupsAndAccountCustomers(t *testing.T) {
	t.Parallel()
	admin := `, "adminSecretRef": {"namespace": "n", "name": "s", "key": "k"}`
	cfg, err := parseIdentities(t, admin, `"iris": {
		"url": "http://iris", "apiKeySecretRef": {"namespace": "n", "name": "i", "key": "k"},
		"groups": [{"name": "Wazuh alert intake", "permissions": ["alerts_write", "customers_read"]}],
		"serviceAccounts": [{"login": "svc_wazuh_a1", "create": true, "customers": ["A"]}, {"login": "svc_ai"}]}`)
	require.NoError(t, err)
	assert.Equal(t, 0x48, cfg.Components.IRIS.Groups[0].PermissionMask())
	assert.Equal(t, []string{"A"}, cfg.Components.IRIS.ServiceAccounts[0].Customers)
	assert.Nil(t, cfg.Components.IRIS.ServiceAccounts[1].Customers, "unset means every tenant")

	_, err = parseIdentities(t, admin, `"iris": {
		"url": "http://iris", "apiKeySecretRef": {"namespace": "n", "name": "i", "key": "k"},
		"groups": [{"name": "G", "permissions": ["alerts_write", "nope", "server_administrator"]}, {"name": "G", "permissions": []}],
		"serviceAccounts": [{"login": "svc_x", "customers": ["Not A Tenant"]}]}`)
	require.Error(t, err)
	for _, want := range []string{
		`unknown IRIS permission "nope"`, "server_administrator is not for", `name "G" is not unique`,
		"permissions must not be empty", `"Not A Tenant" is not a tenant name`,
	} {
		assert.Contains(t, err.Error(), want)
	}
}

func TestMISPUsers(t *testing.T) {
	t.Parallel()
	admin := `, "adminSecretRef": {"namespace": "n", "name": "s", "key": "k"}`
	cfg, err := parseIdentities(t, admin, `"misp": {
		"url": "http://misp", "apiKeySecretRef": {"namespace": "n", "name": "misp-api-key", "key": "key"},
		"users": [{"email": "ioc-export@example.com", "apiKeySecretRef": {"namespace": "n", "name": "misp-export-key", "key": "key"}}]}`)
	require.NoError(t, err)
	assert.Equal(t, DefaultMISPRole, cfg.Components.MISP.Users[0].Role)

	_, err = parseIdentities(t, admin, `"misp": {
		"url": "", "apiKeySecretRef": {"namespace": "n", "name": "misp-api-key", "key": "key"},
		"users": [{"email": "nope", "apiKeySecretRef": {"namespace": "n", "name": "misp-api-key", "key": "key"}}]}`)
	require.Error(t, err)
	for _, want := range []string{"components.misp.url is required", `"nope" is not an e-mail address`, "must not be the admin key"} {
		assert.Contains(t, err.Error(), want)
	}
}
