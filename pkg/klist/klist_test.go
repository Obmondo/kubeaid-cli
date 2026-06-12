// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package klist_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-cli/pkg/klist"
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

func TestLoad_SingleIssuer_NoName(t *testing.T) {
	t.Parallel()

	const clusterYAML = `
name: demo01
server: https://k8s-demo01.netbird:6443
caBundle: "CERT"
oidc:
  - issuerUrl: https://keycloak.acme.com/realms/clusters
    clientId: kubernetes-demo01
`
	reg := makeRegistry(t)
	writeFile(t, reg, "clusters/acme/demo01.yaml", clusterYAML)

	cfg, err := klist.Load(reg, "demo01", "acme")
	require.NoError(t, err)

	assert.Equal(t, "demo01", cfg.Name)
	require.Len(t, cfg.OIDC, 1)
	assert.Equal(t, "https://keycloak.acme.com/realms/clusters", cfg.OIDC[0].IssuerURL)
	assert.Equal(t, "kubernetes-demo01", cfg.OIDC[0].ClientID)
	// Built-in claim defaults for omitted fields.
	assert.Equal(t, "groups", cfg.OIDC[0].GroupsClaim)
	assert.Equal(t, "email", cfg.OIDC[0].UsernameClaim)
	// A single-issuer cluster need not name its issuer, and still validates.
	assert.NoError(t, cfg.Validate())
}

func TestLoad_MultipleIssuers_CustomerConcat(t *testing.T) {
	t.Parallel()

	// Customer-level issuers come first, then the cluster's own.
	const customerYAML = `
customer: acme
displayName: Acme Placeholder
oidc:
  - name: obmondo-sre
    issuerUrl: https://keycloak.obmondo-host.example/realms/obmondo
    clientId: demo01
`
	const clusterYAML = `
name: demo01
server: https://k8s-demo01.netbird:6443
caBundle: "CERT"
oidc:
  - name: customer
    issuerUrl: https://keycloak.acme.com/realms/clusters
    clientId: kubernetes-demo01
`
	reg := makeRegistry(t)
	writeFile(t, reg, "clusters/acme/_customer.yaml", customerYAML)
	writeFile(t, reg, "clusters/acme/demo01.yaml", clusterYAML)

	cfg, err := klist.Load(reg, "demo01", "acme")
	require.NoError(t, err)

	require.Len(t, cfg.OIDC, 2)
	// Concatenation order: customer defaults first, cluster issuers after.
	assert.Equal(t, "obmondo-sre", cfg.OIDC[0].Name)
	assert.Equal(t, "customer", cfg.OIDC[1].Name)
	assert.Equal(t, "demo01", cfg.OIDC[0].ClientID)
	assert.Equal(t, "kubernetes-demo01", cfg.OIDC[1].ClientID)
	assert.NoError(t, cfg.Validate())
}

func TestLoad_ClaimDefaults_PerEntry(t *testing.T) {
	t.Parallel()

	const clusterYAML = `
name: demo01
server: https://k8s-demo01.netbird:6443
caBundle: "CERT"
oidc:
  - issuerUrl: https://keycloak.acme.com/realms/clusters
    clientId: kubernetes-demo01
    groupsClaim: k8s-groups
`
	reg := makeRegistry(t)
	writeFile(t, reg, "clusters/acme/demo01.yaml", clusterYAML)

	cfg, err := klist.Load(reg, "demo01", "acme")
	require.NoError(t, err)

	require.Len(t, cfg.OIDC, 1)
	// Explicit groupsClaim preserved; usernameClaim falls back to the default.
	assert.Equal(t, "k8s-groups", cfg.OIDC[0].GroupsClaim)
	assert.Equal(t, "email", cfg.OIDC[0].UsernameClaim)
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

	const clusterYAML = `
name: demo
server: https://k8s-demo.netbird:6443
caBundle: "CERT"
oidc:
  - issuerUrl: https://keycloak.acme.com/realms/clusters
    clientId: kubernetes-demo
`
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

func TestLoad_ResolvesByYAMLName(t *testing.T) {
	t.Parallel()

	reg := makeRegistry(t)
	// File is named oldfile.yaml, but the cluster's identity is its `name:`
	// field (prod) — e.g. kept in sync with its NetBird peer FQDN
	// prod.netbird.acme.com while the file keeps a legacy name.
	writeFile(t, reg, "clusters/acme/oldfile.yaml", `
name: prod
server: https://prod.netbird.acme.com:6443
caBundle: "CERT"
oidc:
  - issuerUrl: https://keycloak.acme.com/realms/acme
    clientId: kubernetes-prod
`)

	// Resolves by the in-YAML name, not the filename.
	cfg, err := klist.Load(reg, "prod", "acme")
	require.NoError(t, err)
	assert.Equal(t, "prod", cfg.Name)
	assert.Equal(t, "https://prod.netbird.acme.com:6443", cfg.Server)

	// The filename still resolves the same file (backward compatible for
	// registries that never set `name:` or whose name equals the stem).
	byFilename, err := klist.Load(reg, "oldfile", "acme")
	require.NoError(t, err)
	assert.Equal(t, "prod", byFilename.Name)
}

func TestValidate(t *testing.T) {
	t.Parallel()

	newBase := func() *klist.ClusterConfig {
		return &klist.ClusterConfig{
			Name:     "demo",
			Server:   "https://k8s.example.com:6443",
			CABundle: "CERT",
			OIDC: []klist.OIDCIssuer{
				{
					IssuerURL: "https://kc.acme.com/realms/k8s",
					ClientID:  "kubernetes-demo",
				},
			},
		}
	}

	t.Run("valid single issuer without name", func(t *testing.T) {
		t.Parallel()
		assert.NoError(t, newBase().Validate())
	})

	t.Run("valid multiple named issuers", func(t *testing.T) {
		t.Parallel()
		c := newBase()
		c.OIDC = []klist.OIDCIssuer{
			{Name: "customer", IssuerURL: "https://a.acme.com/realms/x", ClientID: "a"},
			{Name: "obmondo-sre", IssuerURL: "https://b.acme.com/realms/y", ClientID: "b"},
		}
		assert.NoError(t, c.Validate())
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
			name:      "no issuers",
			mutate:    func(c *klist.ClusterConfig) { c.OIDC = nil },
			wantField: "oidc",
		},
		{
			name:      "issuer missing issuerUrl",
			mutate:    func(c *klist.ClusterConfig) { c.OIDC[0].IssuerURL = "" },
			wantField: "issuerUrl",
		},
		{
			name:      "issuer missing clientId",
			mutate:    func(c *klist.ClusterConfig) { c.OIDC[0].ClientID = "" },
			wantField: "clientId",
		},
		{
			name: "multiple issuers, one unnamed",
			mutate: func(c *klist.ClusterConfig) {
				c.OIDC = []klist.OIDCIssuer{
					{Name: "customer", IssuerURL: "https://a.acme.com/realms/x", ClientID: "a"},
					{IssuerURL: "https://b.acme.com/realms/y", ClientID: "b"},
				}
			},
			wantField: "name",
		},
		{
			name: "multiple issuers, duplicate names",
			mutate: func(c *klist.ClusterConfig) {
				c.OIDC = []klist.OIDCIssuer{
					{Name: "dup", IssuerURL: "https://a.acme.com/realms/x", ClientID: "a"},
					{Name: "dup", IssuerURL: "https://b.acme.com/realms/y", ClientID: "b"},
				}
			},
			wantField: "duplicate",
		},
	}

	for _, tc := range missingTests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			c := newBase()
			tc.mutate(c)

			err := c.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantField)
		})
	}
}
