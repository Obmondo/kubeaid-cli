// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package parser

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
)

const standaloneGeneralConfig = `forkURLs:
  kubeaid:
    url: https://github.com/example/KubeAid
    version: 1.0.0
  kubeaidConfig:
    url: https://git.example.com/example/kubeaid-config.git
    directory: demo-example-com
cloud:
  hetzner:
    mode: bare-metal
cluster:
  type: workload
  name: demo
  securityOperations:
    enabled: true
    domain: example.com
    keycloak: {url: https://keycloak.example.com/auth}
    agentHost: agents.example.com
    agentAddress: 192.0.2.10
    tenants:
      - {code: "101", name: Tenant A}
`

// LoadSecurityOperationsConfig needs no secrets.yaml and no cloud
// credentials, and applies the security operations defaults.
func TestLoadSecurityOperationsConfig(t *testing.T) {
	origGeneral, origSecrets := config.ParsedGeneralConfig, config.ParsedSecretsConfig
	t.Cleanup(func() {
		config.ParsedGeneralConfig = origGeneral
		config.ParsedSecretsConfig = origSecrets
	})

	path := filepath.Join(t.TempDir(), "kubeaid-cli.general.yaml")
	require.NoError(t, os.WriteFile(path, []byte(standaloneGeneralConfig), 0o600))
	require.NoError(t, LoadSecurityOperationsConfig(path))

	soc := config.ParsedGeneralConfig.Cluster.SecurityOperations
	assert.Equal(t, "soc", soc.Keycloak.Realm)
	assert.Equal(t, 21015, soc.Tenants[0].AgentPorts.Registration)
	assert.Equal(t, "demo-example-com", config.ParsedGeneralConfig.Forks.KubeaidConfigFork.Directory)

	disabled := filepath.Join(t.TempDir(), "off.yaml")
	const offConfig = `forkURLs:
  kubeaid: {url: https://github.com/example/KubeAid}
  kubeaidConfig: {url: https://git.example.com/example/c.git}
cluster: {name: demo}
`
	require.NoError(t, os.WriteFile(disabled, []byte(offConfig), 0o600))
	assert.ErrorContains(t, LoadSecurityOperationsConfig(disabled), "not enabled")
}

func TestNewWazuhCredentials(t *testing.T) {
	central, err := NewWazuhCredentials(false)
	require.NoError(t, err)
	assert.Empty(t, central.APIPassword)
	require.NoError(t, bcrypt.CompareHashAndPassword([]byte(central.IndexerPasswordHash), []byte(central.IndexerPassword)))

	tenant, err := NewWazuhCredentials(true)
	require.NoError(t, err)
	assert.Regexp(t, `^Aa1\.[A-Za-z0-9]{28}$`, tenant.APIPassword)
	assert.NotEmpty(t, tenant.AuthdPassword)
	assert.NotEmpty(t, tenant.ClusterKey)
	require.NoError(t, bcrypt.CompareHashAndPassword([]byte(tenant.DashboardPasswordHash), []byte(tenant.DashboardPassword)))
}
