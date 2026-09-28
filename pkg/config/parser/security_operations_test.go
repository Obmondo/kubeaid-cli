// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package parser

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
)

const (
	testSOCDomain       = "example.com"
	testSOCAgentHost    = "agents.example.com"
	testSOCAgentAddress = "192.0.2.10"
	testSOCKeycloakURL  = "https://keycloak.example.com/auth"
	testSOCKeycloakDNS  = "keycloak.example.com"
	testSOCTenantA      = "Tenant A"
	testSOCTenantB      = "Tenant B"
)

// withSecurityOperations installs a ParsedGeneralConfig holding cfg (and the
// given cluster keycloak block) for the duration of the test.
func withSecurityOperations(t *testing.T, cfg *config.SecurityOperationsConfig, keycloak *config.KeycloakConfig) {
	t.Helper()

	orig := config.ParsedGeneralConfig
	config.ParsedGeneralConfig = &config.GeneralConfig{}
	config.ParsedGeneralConfig.Cluster.SecurityOperations = cfg
	config.ParsedGeneralConfig.Cluster.Keycloak = keycloak
	t.Cleanup(func() { config.ParsedGeneralConfig = orig })
}

func validSOC(tenants ...config.SecurityOperationsTenant) *config.SecurityOperationsConfig {
	return &config.SecurityOperationsConfig{
		Enabled:      true,
		Domain:       testSOCDomain,
		Keycloak:     config.SecurityOperationsKeycloakConfig{URL: testSOCKeycloakURL},
		AgentHost:    testSOCAgentHost,
		AgentAddress: testSOCAgentAddress,
		Tenants:      tenants,
	}
}

func TestHydrateSecurityOperationsDefaults(t *testing.T) {
	t.Run("absent block is a no-op", func(t *testing.T) {
		withSecurityOperations(t, nil, nil)
		hydrateSecurityOperationsDefaults()
		assert.Nil(t, config.ParsedGeneralConfig.Cluster.SecurityOperations)
	})

	t.Run("defaults and derived agent ports", func(t *testing.T) {
		cfg := &config.SecurityOperationsConfig{
			Enabled: true,
			Tenants: []config.SecurityOperationsTenant{
				{Code: "001", Name: testSOCTenantA},
				{Code: "012", Name: testSOCTenantB, IndexerReplicas: 3},
				{Code: "abc", Name: "Tenant C"},
			},
		}
		withSecurityOperations(t, cfg, &config.KeycloakConfig{DNS: testSOCKeycloakDNS})

		hydrateSecurityOperationsDefaults()

		assert.Equal(t, "soc", cfg.Keycloak.Realm)
		assert.Equal(t, testSOCKeycloakURL, cfg.Keycloak.URL,
			"the URL defaults to cluster.keycloak.dns with the /auth context path")
		assert.Equal(t, 20000, cfg.AgentPortBase)
		assert.Equal(t, "traefik", cfg.IngressClassName)
		assert.Equal(t, constants.ClusterIssuerLetsEncrypt, cfg.ClusterIssuer)
		require.NotNil(t, cfg.Reconciler.DryRun)
		assert.True(t, *cfg.Reconciler.DryRun, "the reconciler starts as a dry run")
		assert.False(t, cfg.Sync.Automated, "the SOC Applications sync by hand by default")
		require.NotNil(t, cfg.Sync.ServerSideApply)
		assert.False(t, *cfg.Sync.ServerSideApply, "ServerSideApply follows automated")

		assert.Equal(t, 1, cfg.Tenants[0].IndexerReplicas)
		assert.Equal(t, 3, cfg.Tenants[1].IndexerReplicas)

		assert.Equal(t, &config.SecurityOperationsAgentPorts{Registration: 20015, Events: 20014},
			cfg.Tenants[0].AgentPorts)
		assert.Equal(t, &config.SecurityOperationsAgentPorts{Registration: 20125, Events: 20124},
			cfg.Tenants[1].AgentPorts)
		assert.Nil(t, cfg.Tenants[2].AgentPorts, "no ports are derived from a non-numeric code")
	})

	t.Run("explicit values win", func(t *testing.T) {
		dryRun := false
		cfg := &config.SecurityOperationsConfig{
			Keycloak:      config.SecurityOperationsKeycloakConfig{URL: "https://sso.example.com", Realm: "sec"},
			AgentPortBase: 30000,
			Reconciler:    config.SecurityOperationsReconcilerConfig{DryRun: &dryRun},
			Tenants: []config.SecurityOperationsTenant{
				{Code: "002", AgentPorts: &config.SecurityOperationsAgentPorts{Registration: 1515, Events: 1514}},
				{Code: "003"},
			},
		}
		withSecurityOperations(t, cfg, &config.KeycloakConfig{DNS: testSOCKeycloakDNS})

		hydrateSecurityOperationsDefaults()

		assert.Equal(t, "https://sso.example.com", cfg.Keycloak.URL)
		assert.Equal(t, "sec", cfg.Keycloak.Realm)
		assert.False(t, *cfg.Reconciler.DryRun)
		assert.Equal(t, 1515, cfg.Tenants[0].AgentPorts.Registration)
		assert.Equal(t, 30035, cfg.Tenants[1].AgentPorts.Registration)
		assert.Equal(t, 30034, cfg.Tenants[1].AgentPorts.Events)
	})

	t.Run("no keycloak URL without cluster.keycloak", func(t *testing.T) {
		cfg := &config.SecurityOperationsConfig{Enabled: true}
		withSecurityOperations(t, cfg, nil)
		hydrateSecurityOperationsDefaults()
		assert.Empty(t, cfg.Keycloak.URL)
	})
}

func TestValidateSecurityOperationsConfig(t *testing.T) {
	tenantA := config.SecurityOperationsTenant{Code: "001", Name: testSOCTenantA}
	tenantB := config.SecurityOperationsTenant{Code: "002", Name: testSOCTenantB}

	tests := []struct {
		name    string
		mutate  func(cfg *config.SecurityOperationsConfig)
		wantErr string
	}{
		{name: "valid", mutate: func(*config.SecurityOperationsConfig) {}},
		{
			name: "content canary must be a tenant",
			mutate: func(cfg *config.SecurityOperationsConfig) {
				cfg.Content = config.SecurityOperationsContentConfig{Enabled: true, Canary: "009"}
			},
			wantErr: `content.canary "009"`,
		},
		{
			name: "content extra list must be a flat etc/lists path",
			mutate: func(cfg *config.SecurityOperationsConfig) {
				cfg.Content = config.SecurityOperationsContentConfig{Enabled: true, ExtraLists: []string{"etc/lists/a/b"}}
			},
			wantErr: "content.extraLists",
		},
		{
			name: "content valid",
			mutate: func(cfg *config.SecurityOperationsConfig) {
				cfg.Content = config.SecurityOperationsContentConfig{Enabled: true, Canary: "002", ExtraLists: []string{"etc/lists/kubesoc-x"}}
			},
		},
		{
			name:   "disabled block is not checked",
			mutate: func(cfg *config.SecurityOperationsConfig) { cfg.Enabled = false; cfg.Domain = "" },
		},
		{
			name:    "domain required",
			mutate:  func(cfg *config.SecurityOperationsConfig) { cfg.Domain = "" },
			wantErr: "domain",
		},
		{
			name:    "keycloak url required",
			mutate:  func(cfg *config.SecurityOperationsConfig) { cfg.Keycloak.URL = "" },
			wantErr: "keycloak.url is required",
		},
		{
			name:    "keycloak url must be a URL",
			mutate:  func(cfg *config.SecurityOperationsConfig) { cfg.Keycloak.URL = "keycloak.example.com" },
			wantErr: "keycloak.url",
		},
		{
			name:    "agentHost required",
			mutate:  func(cfg *config.SecurityOperationsConfig) { cfg.AgentHost = "" },
			wantErr: "agentHost",
		},
		{
			name:    "agentAddress must be an IP",
			mutate:  func(cfg *config.SecurityOperationsConfig) { cfg.AgentAddress = "agents.example.com" },
			wantErr: "agentAddress",
		},
		{
			name: "bad tenant code",
			mutate: func(cfg *config.SecurityOperationsConfig) {
				cfg.Tenants[0].Code = "Tenant_1"
			},
			wantErr: "code must match",
		},
		{
			name: "duplicate code",
			mutate: func(cfg *config.SecurityOperationsConfig) {
				cfg.Tenants[1].Code = "001"
				cfg.Tenants[1].AgentPorts = &config.SecurityOperationsAgentPorts{Registration: 30001, Events: 30002}
			},
			wantErr: `code "001" is used twice`,
		},
		{
			name:    "duplicate name",
			mutate:  func(cfg *config.SecurityOperationsConfig) { cfg.Tenants[1].Name = testSOCTenantA },
			wantErr: `name "Tenant A" is used twice`,
		},
		{
			name:    "name required",
			mutate:  func(cfg *config.SecurityOperationsConfig) { cfg.Tenants[1].Name = "" },
			wantErr: "name is required",
		},
		{
			name:    "template delimiters in a name",
			mutate:  func(cfg *config.SecurityOperationsConfig) { cfg.Tenants[1].Name = "Tenant {{ B }}" },
			wantErr: "must not contain",
		},
		{
			name: "non-numeric code needs agentPorts",
			mutate: func(cfg *config.SecurityOperationsConfig) {
				cfg.Tenants[1].Code = "globex"
				cfg.Tenants[1].AgentPorts = nil
			},
			wantErr: "agentPorts is required",
		},
		{
			name: "port used twice",
			mutate: func(cfg *config.SecurityOperationsConfig) {
				cfg.Tenants[1].AgentPorts = &config.SecurityOperationsAgentPorts{Registration: 20015, Events: 20024}
			},
			wantErr: "port 20015 is already used by tenant 001 registration",
		},
		{
			name: "registration and events equal",
			mutate: func(cfg *config.SecurityOperationsConfig) {
				cfg.Tenants[1].AgentPorts = &config.SecurityOperationsAgentPorts{Registration: 30000, Events: 30000}
			},
			wantErr: "port 30000 is already used",
		},
		{
			name: "port out of range",
			mutate: func(cfg *config.SecurityOperationsConfig) {
				cfg.Tenants[1].AgentPorts = &config.SecurityOperationsAgentPorts{Registration: 70000, Events: 30000}
			},
			wantErr: "outside 1-65535",
		},
		{
			name:    "sync.prune needs sync.automated",
			mutate:  func(cfg *config.SecurityOperationsConfig) { cfg.Sync.Prune = true },
			wantErr: "sync.prune needs sync.automated",
		},
		{
			name: "automated sync with prune",
			mutate: func(cfg *config.SecurityOperationsConfig) {
				cfg.Sync = config.SecurityOperationsSyncConfig{Automated: true, Prune: true}
			},
		},
		{
			name: "derived port past 65535",
			mutate: func(cfg *config.SecurityOperationsConfig) {
				cfg.AgentPortBase = 65000
				cfg.Tenants[1].Code = "999"
				cfg.Tenants[1].AgentPorts = nil
			},
			wantErr: "outside 1-65535",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validSOC(tenantA, tenantB)
			withSecurityOperations(t, cfg, nil)
			tc.mutate(cfg)
			hydrateSecurityOperationsDefaults()

			err := validateSecurityOperationsConfig()
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func sizingSOC() *config.SecurityOperationsConfig {
	return validSOC(
		config.SecurityOperationsTenant{Code: "001", Name: testSOCTenantA},
		config.SecurityOperationsTenant{Code: "002", Name: testSOCTenantB, ExpectedGBPerDay: 2, IndexerStorageSize: "80Gi"},
	)
}

func TestSecurityOperationsIndexerSizingFields(t *testing.T) {
	// Defaults: expectedGBPerDay is filled in, the override stays as given.
	cfg := sizingSOC()
	withSecurityOperations(t, cfg, nil)
	hydrateSecurityOperationsDefaults()
	require.NoError(t, validateSecurityOperationsConfig())
	assert.InDelta(t, constants.SecurityOperationsDefaultGBPerDay, cfg.Tenants[0].ExpectedGBPerDay, 0.001)
	assert.InDelta(t, 2.0, cfg.Tenants[1].ExpectedGBPerDay, 0.001)
	assert.Equal(t, "80Gi", cfg.Tenants[1].IndexerStorageSize)

	// A malformed override is rejected.
	cfg = sizingSOC()
	cfg.Tenants[0].IndexerStorageSize = "80G"
	withSecurityOperations(t, cfg, nil)
	hydrateSecurityOperationsDefaults()
	err := validateSecurityOperationsConfig()
	require.Error(t, err)
	assert.Contains(t, err.Error(), `indexerStorageSize "80G" must look like 80Gi`)

	cfg = sizingSOC()
	cfg.Tenants[0].ExpectedGBPerDay = -1
	withSecurityOperations(t, cfg, nil)
	hydrateSecurityOperationsDefaults()
	err = validateSecurityOperationsConfig()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expectedGBPerDay must not be negative")
}
