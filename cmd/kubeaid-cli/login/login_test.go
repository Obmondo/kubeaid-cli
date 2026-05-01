// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package login

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/klist"
)

// --------------------------------------------------------------------------
// resolveInput
// --------------------------------------------------------------------------

func TestResolveInput(t *testing.T) {
	// t.Setenv is incompatible with t.Parallel on subtests; run sequentially.
	tests := []struct {
		name     string
		flagVal  string
		envKey   string
		envVal   string
		dflt     string
		expected string
	}{
		{
			name:     "flag wins over env and default",
			flagVal:  "/flag/path",
			envKey:   "TEST_RESOLVE_1",
			envVal:   "/env/path",
			dflt:     "/default/path",
			expected: "/flag/path",
		},
		{
			name:     "env wins over default when flag empty",
			flagVal:  "",
			envKey:   "TEST_RESOLVE_2",
			envVal:   "/env/path",
			dflt:     "/default/path",
			expected: "/env/path",
		},
		{
			name:     "default used when both flag and env are empty",
			flagVal:  "",
			envKey:   "TEST_RESOLVE_3",
			envVal:   "",
			dflt:     "/default/path",
			expected: "/default/path",
		},
		{
			name:     "empty env var is treated as absent",
			flagVal:  "",
			envKey:   "TEST_RESOLVE_4",
			envVal:   "",
			dflt:     "/fallback",
			expected: "/fallback",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if tc.envVal != "" {
				t.Setenv(tc.envKey, tc.envVal)
			} else {
				os.Unsetenv(tc.envKey)
			}

			got := resolveInput(tc.flagVal, tc.envKey, tc.dflt)
			assert.Equal(t, tc.expected, got)
		})
	}
}

// --------------------------------------------------------------------------
// expandTilde
// --------------------------------------------------------------------------

func TestExpandTilde(t *testing.T) {
	t.Parallel()

	home, err := os.UserHomeDir()
	require.NoError(t, err)

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "tilde prefix is expanded",
			input: "~/.kubeaid/cert.pem",
			want:  filepath.Join(home, ".kubeaid/cert.pem"),
		},
		{
			name:  "absolute path is unchanged",
			input: "/etc/ssl/certs/cert.pem",
			want:  "/etc/ssl/certs/cert.pem",
		},
		{
			name:  "relative path is unchanged",
			input: "relative/path",
			want:  "relative/path",
		},
		{
			name:  "bare tilde without slash is unchanged",
			input: "~",
			want:  "~",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, expandTilde(tc.input))
		})
	}
}

// --------------------------------------------------------------------------
// buildKubeconfig
// --------------------------------------------------------------------------

func TestBuildKubeconfig(t *testing.T) {
	t.Parallel()

	cfg := &klist.ClusterConfig{
		Name:     "demo01",
		Server:   "https://k8s-demo01.netbird:6443",
		CABundle: "FAKE-CA-CERT",
		OIDC: klist.OIDCConfig{
			IssuerURL:     "https://keycloak.example.com/realms/clusters",
			ClientID:      "kubernetes-demo01",
			GroupsClaim:   "groups",
			UsernameClaim: "email",
		},
	}

	data, err := buildKubeconfig(cfg, "demo01", "samplec5t9")
	require.NoError(t, err)

	var kc kubeconfig
	require.NoError(t, yaml.Unmarshal(data, &kc))

	contextName := "demo01.samplec5t9"

	assert.Equal(t, "v1", kc.APIVersion)
	assert.Equal(t, "Config", kc.Kind)
	assert.Equal(t, contextName, kc.CurrentContext)

	require.Len(t, kc.Clusters, 1)
	assert.Equal(t, contextName, kc.Clusters[0].Name)
	assert.Equal(t, "https://k8s-demo01.netbird:6443", kc.Clusters[0].Cluster.Server)
	wantCA := base64.StdEncoding.EncodeToString([]byte("FAKE-CA-CERT"))
	assert.Equal(t, wantCA, kc.Clusters[0].Cluster.CertificateAuthorityData)

	require.Len(t, kc.Contexts, 1)
	assert.Equal(t, contextName, kc.Contexts[0].Name)
	assert.Equal(t, contextName, kc.Contexts[0].Context.Cluster)
	assert.Equal(t, "oidc", kc.Contexts[0].Context.User)

	require.Len(t, kc.Users, 1)
	assert.Equal(t, "oidc", kc.Users[0].Name)

	exec := kc.Users[0].User.Exec
	assert.Equal(t, "client.authentication.k8s.io/v1beta1", exec.APIVersion)
	assert.Equal(t, "kubelogin", exec.Command)
	assert.Contains(t, exec.Args, "get-token")
	assert.Contains(t, exec.Args, "--oidc-issuer-url=https://keycloak.example.com/realms/clusters")
	assert.Contains(t, exec.Args, "--oidc-client-id=kubernetes-demo01")
	assert.Contains(t, exec.Args, "--oidc-extra-scope=email")
	assert.Contains(t, exec.Args, "--oidc-extra-scope=groups")

	// No client cert or client key data anywhere in the output.
	rawYAML := string(data)
	assert.NotContains(t, rawYAML, "client-certificate-data")
	assert.NotContains(t, rawYAML, "client-key-data")
}

// --------------------------------------------------------------------------
// writeKubeconfig
// --------------------------------------------------------------------------

func TestWriteKubeconfig(t *testing.T) {
	t.Parallel()

	t.Run("creates intermediate directories and writes with 0600", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		outPath := filepath.Join(dir, "nested", "subdir", "config")

		content := []byte("kubeconfig content")
		require.NoError(t, writeKubeconfig(outPath, content))

		got, err := os.ReadFile(outPath)
		require.NoError(t, err)
		assert.Equal(t, content, got)

		info, err := os.Stat(outPath)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	})

	t.Run("overwrites existing file", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		outPath := filepath.Join(dir, "config")

		require.NoError(t, os.WriteFile(outPath, []byte("old"), 0o600))
		require.NoError(t, writeKubeconfig(outPath, []byte("new")))

		got, err := os.ReadFile(outPath)
		require.NoError(t, err)
		assert.Equal(t, []byte("new"), got)
	})
}
