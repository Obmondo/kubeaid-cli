// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package klist_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/klist"
)

// makeRegistry creates a minimal registry directory tree under a temp dir and
// returns the registry root path.
func makeRegistry(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

// writeFile writes content to registryRoot/rel, creating parent dirs.
func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o750))
	require.NoError(t, os.WriteFile(full, []byte(content), 0o600))
}

const customerYAML = `
customer: samplec5t9
displayName: Acme Placeholder
oidc:
  issuerUrl: https://keycloak.example.com/realms/clusters
  groupsClaim: groups
  usernameClaim: email
`

const clusterYAML = `
name: demo01
server: https://k8s-demo01.netbird:6443
caBundle: |
  -----BEGIN CERTIFICATE-----
  MIID...
  -----END CERTIFICATE-----
oidc:
  clientId: kubernetes-demo01
allowedGroups:
  - k8s-demo01-dev
`

func TestLoad_WithCustomerDefaults(t *testing.T) {
	t.Parallel()

	reg := makeRegistry(t)
	writeFile(t, reg, "clusters/samplec5t9/_customer.yaml", customerYAML)
	writeFile(t, reg, "clusters/samplec5t9/demo01.yaml", clusterYAML)

	cfg, err := klist.Load(reg, "demo01", "samplec5t9")
	require.NoError(t, err)

	assert.Equal(t, "demo01", cfg.Name)
	assert.Equal(t, "https://k8s-demo01.netbird:6443", cfg.Server)
	// issuerUrl comes from _customer.yaml (cluster omits it)
	assert.Equal(t, "https://keycloak.example.com/realms/clusters", cfg.OIDC.IssuerURL)
	// clientId comes from cluster YAML
	assert.Equal(t, "kubernetes-demo01", cfg.OIDC.ClientID)
	// groupsClaim from customer
	assert.Equal(t, "groups", cfg.OIDC.GroupsClaim)
	// usernameClaim from customer
	assert.Equal(t, "email", cfg.OIDC.UsernameClaim)
}

func TestLoad_WithoutCustomerDefaults(t *testing.T) {
	t.Parallel()

	const fullClusterYAML = `
name: standalone
server: https://k8s-standalone.netbird:6443
caBundle: "CERT"
oidc:
  issuerUrl: https://keycloak.example.com/realms/clusters
  clientId: kubernetes-standalone
`
	reg := makeRegistry(t)
	writeFile(t, reg, "clusters/custid/standalone.yaml", fullClusterYAML)
	// No _customer.yaml.

	cfg, err := klist.Load(reg, "standalone", "custid")
	require.NoError(t, err)

	assert.Equal(t, "standalone", cfg.Name)
	// Built-in defaults for omitted claim fields.
	assert.Equal(t, "groups", cfg.OIDC.GroupsClaim)
	assert.Equal(t, "email", cfg.OIDC.UsernameClaim)
}

func TestLoad_ClusterOverridesCustomer(t *testing.T) {
	t.Parallel()

	const overrideCluster = `
name: prod
server: https://k8s-prod.netbird:6443
caBundle: "CERT"
oidc:
  issuerUrl: https://other-keycloak.example.com/realms/prod
  clientId: kubernetes-prod
  groupsClaim: k8s-groups
`
	reg := makeRegistry(t)
	writeFile(t, reg, "clusters/cust/prod.yaml", overrideCluster)
	writeFile(t, reg, "clusters/cust/_customer.yaml", customerYAML)

	cfg, err := klist.Load(reg, "prod", "cust")
	require.NoError(t, err)

	// Cluster issuerUrl overrides customer's.
	assert.Equal(t, "https://other-keycloak.example.com/realms/prod", cfg.OIDC.IssuerURL)
	// Cluster groupsClaim overrides customer's.
	assert.Equal(t, "k8s-groups", cfg.OIDC.GroupsClaim)
	// usernameClaim not set in cluster, falls back to customer value.
	assert.Equal(t, "email", cfg.OIDC.UsernameClaim)
}

func TestLoad_MissingClusterFile(t *testing.T) {
	t.Parallel()

	reg := makeRegistry(t)

	_, err := klist.Load(reg, "missing", "custid")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestLoad_MalformedCustomerYAML(t *testing.T) {
	t.Parallel()

	reg := makeRegistry(t)
	writeFile(t, reg, "clusters/cust/_customer.yaml", ":\tinvalid: yaml: {")
	writeFile(t, reg, "clusters/cust/demo.yaml", clusterYAML)

	_, err := klist.Load(reg, "demo", "cust")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing customer defaults")
}

func TestLoad_MalformedClusterYAML(t *testing.T) {
	t.Parallel()

	reg := makeRegistry(t)
	writeFile(t, reg, "clusters/cust/demo.yaml", ":\tinvalid: yaml: {")

	_, err := klist.Load(reg, "demo", "cust")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing cluster config")
}

func TestValidate(t *testing.T) {
	t.Parallel()

	base := &klist.ClusterConfig{
		Name:     "demo",
		Server:   "https://k8s.example.com:6443",
		CABundle: "CERT",
		OIDC: klist.OIDCConfig{
			IssuerURL: "https://kc.example.com/realms/k8s",
			ClientID:  "kubernetes-demo",
		},
	}

	t.Run("valid config", func(t *testing.T) {
		t.Parallel()
		assert.NoError(t, base.Validate())
	})

	missingTests := []struct {
		name      string
		mutate    func(*klist.ClusterConfig)
		wantField string
	}{
		{
			name:      "missing name",
			mutate:    func(c *klist.ClusterConfig) { c.Name = "" },
			wantField: "name",
		},
		{
			name:      "missing server",
			mutate:    func(c *klist.ClusterConfig) { c.Server = "" },
			wantField: "server",
		},
		{
			name:      "missing caBundle",
			mutate:    func(c *klist.ClusterConfig) { c.CABundle = "" },
			wantField: "caBundle",
		},
		{
			name:      "missing oidc.issuerUrl",
			mutate:    func(c *klist.ClusterConfig) { c.OIDC.IssuerURL = "" },
			wantField: "oidc.issuerUrl",
		},
		{
			name:      "missing oidc.clientId",
			mutate:    func(c *klist.ClusterConfig) { c.OIDC.ClientID = "" },
			wantField: "oidc.clientId",
		},
	}

	for _, tc := range missingTests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Deep-copy the base struct.
			cp := *base
			oidcCp := cp.OIDC
			cp.OIDC = oidcCp
			tc.mutate(&cp)

			err := cp.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantField)
		})
	}
}
