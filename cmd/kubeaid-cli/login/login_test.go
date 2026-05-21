// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package login

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/Obmondo/kubeaid-cli/pkg/klist"
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
		t.Run(tc.name, func(t *testing.T) {
			if tc.envVal != "" {
				t.Setenv(tc.envKey, tc.envVal)
			} else {
				_ = os.Unsetenv(tc.envKey)
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
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, expandTilde(tc.input))
		})
	}
}

// --------------------------------------------------------------------------
// upsertCluster
// --------------------------------------------------------------------------

func TestUpsertCluster_IntoEmptyKubeconfig(t *testing.T) {
	t.Parallel()

	cfg := &klist.ClusterConfig{
		Name:     "demo01",
		Server:   "https://k8s-demo01.netbird:6443",
		CABundle: "FAKE-CA-CERT",
		OIDC: klist.OIDCConfig{
			IssuerURL: "https://keycloak.example.com/realms/clusters",
			ClientID:  "kubernetes-demo01",
		},
	}

	kc := &kubeconfig{APIVersion: "v1", Kind: "Config"}
	upsertCluster(kc, cfg, "", "demo01", "samplec5t9")

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
	// User name now matches context name (so two clusters don't share
	// a single "oidc" user that would let one stomp the other's exec
	// args).
	assert.Equal(t, contextName, kc.Contexts[0].Context.User)

	require.Len(t, kc.Users, 1)
	assert.Equal(t, contextName, kc.Users[0].Name)

	exec := kc.Users[0].User.Exec
	assert.Equal(t, "client.authentication.k8s.io/v1beta1", exec.APIVersion)
	assert.Equal(t, "kubelogin", exec.Command)
	assert.Equal(t, kubeloginArgs(cfg), exec.Args)

	// Round-trip through YAML to verify no client cert / key fields slip in.
	out, err := yaml.Marshal(kc)
	require.NoError(t, err)
	rawYAML := string(out)
	assert.NotContains(t, rawYAML, "client-certificate-data")
	assert.NotContains(t, rawYAML, "client-key-data")
}

func TestUpsertCluster_PreservesOtherEntries(t *testing.T) {
	t.Parallel()

	// Existing kubeconfig has an unrelated context (e.g. the user's
	// kind cluster). Our upsert must leave it untouched.
	kc := &kubeconfig{
		APIVersion:     "v1",
		Kind:           "Config",
		CurrentContext: "kind-local",
		Clusters: []namedCluster{
			{Name: "kind-local", Cluster: clusterInfo{Server: "https://localhost:6443"}},
		},
		Contexts: []namedContext{
			{Name: "kind-local", Context: contextInfo{Cluster: "kind-local", User: "kind"}},
		},
		Users: []namedUser{
			{Name: "kind", User: userInfo{}},
		},
	}

	cfg := &klist.ClusterConfig{
		Server:   "https://k8s-staging.netbird:6443",
		CABundle: "CA",
		OIDC:     klist.OIDCConfig{IssuerURL: "https://kc.example/realms", ClientID: "k8s"},
	}

	upsertCluster(kc, cfg, "", "staging", "acme")

	// Existing entry survives.
	require.Len(t, kc.Clusters, 2)
	assert.Equal(t, "kind-local", kc.Clusters[0].Name)
	assert.Equal(t, "staging.acme", kc.Clusters[1].Name)

	require.Len(t, kc.Contexts, 2)
	require.Len(t, kc.Users, 2)

	// current-context switches to the new entry.
	assert.Equal(t, "staging.acme", kc.CurrentContext)
}

func TestUpsertCluster_ReplacesExistingSameName(t *testing.T) {
	t.Parallel()

	// Re-running login for a cluster that already has an entry must
	// update in place, not append a duplicate.
	kc := &kubeconfig{APIVersion: "v1", Kind: "Config"}

	first := &klist.ClusterConfig{
		Server:   "https://old.example:6443",
		CABundle: "OLD-CA",
		OIDC:     klist.OIDCConfig{IssuerURL: "https://old.example/realms", ClientID: "old"},
	}
	upsertCluster(kc, first, "", "staging", "acme")
	require.Len(t, kc.Clusters, 1)

	second := &klist.ClusterConfig{
		Server:   "https://new.example:6443",
		CABundle: "NEW-CA",
		OIDC:     klist.OIDCConfig{IssuerURL: "https://new.example/realms", ClientID: "new"},
	}
	upsertCluster(kc, second, "", "staging", "acme")

	require.Len(t, kc.Clusters, 1)
	require.Len(t, kc.Contexts, 1)
	require.Len(t, kc.Users, 1)
	assert.Equal(t, "https://new.example:6443", kc.Clusters[0].Cluster.Server)
	assert.Equal(t, kubeloginArgs(second), kc.Users[0].User.Exec.Args)
}

func TestUpsertCluster_AppliesContextPrefix(t *testing.T) {
	t.Parallel()

	kc := &kubeconfig{APIVersion: "v1", Kind: "Config"}
	cfg := &klist.ClusterConfig{
		Server:   "https://k8s-staging.netbird:6443",
		CABundle: "CA",
		OIDC:     klist.OIDCConfig{IssuerURL: "https://kc.example/realms", ClientID: "k8s"},
	}

	upsertCluster(kc, cfg, "kubeaid-", "staging", "acme")

	const want = "kubeaid-staging.acme"

	require.Len(t, kc.Clusters, 1)
	assert.Equal(t, want, kc.Clusters[0].Name)
	require.Len(t, kc.Contexts, 1)
	assert.Equal(t, want, kc.Contexts[0].Name)
	assert.Equal(t, want, kc.Contexts[0].Context.Cluster)
	assert.Equal(t, want, kc.Contexts[0].Context.User)
	require.Len(t, kc.Users, 1)
	assert.Equal(t, want, kc.Users[0].Name)
	assert.Equal(t, want, kc.CurrentContext)
}

// --------------------------------------------------------------------------
// loadKubeconfig
// --------------------------------------------------------------------------

func TestLoadKubeconfig(t *testing.T) {
	t.Parallel()

	t.Run("missing file returns empty Config", func(t *testing.T) {
		t.Parallel()

		kc, err := loadKubeconfig(filepath.Join(t.TempDir(), "nope"))
		require.NoError(t, err)
		assert.Equal(t, "v1", kc.APIVersion)
		assert.Equal(t, "Config", kc.Kind)
		assert.Empty(t, kc.Clusters)
		assert.Empty(t, kc.Contexts)
		assert.Empty(t, kc.Users)
	})

	t.Run("empty file returns empty Config", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "empty")
		require.NoError(t, os.WriteFile(path, []byte(""), 0o600))

		kc, err := loadKubeconfig(path)
		require.NoError(t, err)
		assert.Equal(t, "v1", kc.APIVersion)
		assert.Equal(t, "Config", kc.Kind)
	})

	t.Run("existing file is parsed", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "config")
		require.NoError(t, os.WriteFile(path, []byte(`apiVersion: v1
kind: Config
current-context: foo
clusters:
  - name: foo
    cluster:
      server: https://foo.example:6443
contexts:
  - name: foo
    context:
      cluster: foo
      user: foo
users:
  - name: foo
    user: {}
`), 0o600))

		kc, err := loadKubeconfig(path)
		require.NoError(t, err)
		assert.Equal(t, "foo", kc.CurrentContext)
		require.Len(t, kc.Clusters, 1)
		assert.Equal(t, "https://foo.example:6443", kc.Clusters[0].Cluster.Server)
	})

	t.Run("malformed YAML returns parse error", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "bad")
		require.NoError(t, os.WriteFile(path, []byte("not: yaml: at: all"), 0o600))

		_, err := loadKubeconfig(path)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "parsing kubeconfig")
	})
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

// --------------------------------------------------------------------------
// kubeloginArgs
// --------------------------------------------------------------------------

func TestKubeloginArgs(t *testing.T) {
	t.Parallel()

	cfg := &klist.ClusterConfig{
		OIDC: klist.OIDCConfig{
			IssuerURL: "https://keycloak.example.com/realms/clusters",
			ClientID:  "kubernetes-demo01",
		},
	}

	want := []string{
		"get-token",
		"--oidc-issuer-url=https://keycloak.example.com/realms/clusters",
		"--oidc-client-id=kubernetes-demo01",
		"--oidc-extra-scope=email",
		"--oidc-extra-scope=groups",
	}

	assert.Equal(t, want, kubeloginArgs(cfg))
}

// --------------------------------------------------------------------------
// lookupKubelogin
// --------------------------------------------------------------------------

func TestLookupKubelogin_NotFound(t *testing.T) {
	// t.Setenv is incompatible with t.Parallel; run sequentially.
	t.Setenv("PATH", "")

	path, err := lookupKubelogin()
	require.Error(t, err)
	assert.Empty(t, path)
	assert.Contains(t, err.Error(), kubeloginBinary)
	assert.Contains(t, err.Error(), kubeloginRepo)
	assert.Contains(t, err.Error(), flagNoAuthenticate)
}

func TestLookupKubelogin_Found(t *testing.T) {
	// Stage a fake kubelogin binary in a temp dir and point PATH at it.
	dir := t.TempDir()
	fake := filepath.Join(dir, kubeloginBinary)
	require.NoError(t, os.WriteFile(fake, []byte("#!/bin/sh\nexit 0\n"), 0o755)) //nolint:gosec // G306: test fixture must be executable.

	t.Setenv("PATH", dir)

	got, err := lookupKubelogin()
	require.NoError(t, err)
	assert.Equal(t, fake, got)
}

// --------------------------------------------------------------------------
// runKubelogin
// --------------------------------------------------------------------------

func TestRunKubelogin(t *testing.T) {
	t.Parallel()

	t.Run("success path forwards exit 0", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		fake := filepath.Join(dir, kubeloginBinary)
		require.NoError(t, os.WriteFile(fake, []byte("#!/bin/sh\nexit 0\n"), 0o755)) //nolint:gosec // G306: test fixture must be executable.

		err := runKubelogin(context.Background(), fake, []string{"get-token"})
		assert.NoError(t, err)
	})

	t.Run("failure surfaces a stderr-derived clue (uncategorised path)", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		fake := filepath.Join(dir, kubeloginBinary)
		// stderr "boom" doesn't match any classifyKubeloginErr pattern,
		// so the fallback path that includes the first stderr line
		// fires. The user gets "boom" instead of the cryptic
		// "exit status 7".
		//nolint:gosec // G306: test fixture must be executable.
		require.NoError(t, os.WriteFile(fake,
			[]byte("#!/bin/sh\necho boom >&2\nexit 7\n"), 0o755))

		err := runKubelogin(context.Background(), fake, []string{"get-token"})
		require.Error(t, err)
		assert.Equal(t, "kubelogin: boom", err.Error())
	})
}

func TestFirstNonEmptyLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"  \n\t\n", ""},
		{"hello\n", "hello"},
		{"\n\n  hello world  \n\n", "hello world"},
		{"first\nsecond\n", "first"},
	}

	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, firstNonEmptyLine(tc.in))
		})
	}
}

// --------------------------------------------------------------------------
// intersectClusters
// --------------------------------------------------------------------------

func TestIntersectClusters(t *testing.T) {
	t.Parallel()

	refs := []klist.ClusterRef{
		{Customer: "acme", ClusterName: "staging"},
		{Customer: "acme", ClusterName: "prod"},
		{Customer: "bigcorp", ClusterName: "staging"}, // same name, different customer
		{Customer: "zeta", ClusterName: "dev"},
	}

	tests := []struct {
		name       string
		accessible []string
		want       []klist.ClusterRef
	}{
		{
			name:       "intersects on cluster name and keeps every matching customer copy",
			accessible: []string{"staging", "dev"},
			want: []klist.ClusterRef{
				{Customer: "acme", ClusterName: "staging"},
				{Customer: "bigcorp", ClusterName: "staging"},
				{Customer: "zeta", ClusterName: "dev"},
			},
		},
		{
			name:       "empty accessible → empty result",
			accessible: nil,
			want:       []klist.ClusterRef{},
		},
		{
			name:       "no overlap → empty result",
			accessible: []string{"unknown"},
			want:       []klist.ClusterRef{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := intersectClusters(refs, tc.accessible)
			assert.Equal(t, tc.want, got)
		})
	}
}

// --------------------------------------------------------------------------
// groupByCustomer
// --------------------------------------------------------------------------

func TestGroupByCustomer(t *testing.T) {
	t.Parallel()

	refs := []klist.ClusterRef{
		{Customer: "acme", ClusterName: "staging"},
		{Customer: "acme", ClusterName: "prod"},
		{Customer: "bigcorp", ClusterName: "staging"},
		{Customer: "zeta", ClusterName: "dev"},
	}

	got := groupByCustomer(refs)

	assert.Equal(t, []klist.ClusterRef{
		{Customer: "acme", ClusterName: "staging"},
		{Customer: "acme", ClusterName: "prod"},
	}, got["acme"])
	assert.Equal(t, []klist.ClusterRef{
		{Customer: "bigcorp", ClusterName: "staging"},
	}, got["bigcorp"])
	assert.Equal(t, []klist.ClusterRef{
		{Customer: "zeta", ClusterName: "dev"},
	}, got["zeta"])
	assert.Len(t, got, 3)
}

func TestPickCustomer_AutoSelectsSingleCustomer(t *testing.T) {
	// When only one customer is reachable, the picker must not prompt.
	// We exercise the non-stubbed path; if huh tried to prompt, the test
	// would hang.
	byCustomer := map[string][]klist.ClusterRef{
		"acme": {{Customer: "acme", ClusterName: "staging"}},
	}

	got, err := pickCustomer(byCustomer)
	require.NoError(t, err)
	assert.Equal(t, "acme", got)
}

func TestPickClusterWithin_AutoSelectsSingleCluster(t *testing.T) {
	// Same auto-select shortcut for the within-customer step.
	got, err := pickClusterWithin("acme",
		[]klist.ClusterRef{{Customer: "acme", ClusterName: "prod"}})
	require.NoError(t, err)
	assert.Equal(t, "prod", got)
}

func TestNotFoundWithSuggestions(t *testing.T) {
	t.Parallel()

	t.Run("includes available cluster list", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "clusters/acme"), 0o750))
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "clusters/zeta"), 0o750))
		require.NoError(t, os.WriteFile(
			filepath.Join(dir, "clusters/acme/staging.yaml"), []byte("name: staging"), 0o600,
		))
		require.NoError(t, os.WriteFile(
			filepath.Join(dir, "clusters/zeta/dev.yaml"), []byte("name: dev"), 0o600,
		))

		err := notFoundWithSuggestions(klist.ErrClusterNotFound, dir, "bogus", "acme")
		require.Error(t, err)

		msg := err.Error()
		assert.Contains(t, msg, "bogus.acme")
		assert.Contains(t, msg, "staging.acme")
		assert.Contains(t, msg, "dev.zeta")
	})

	t.Run("registry empty falls back to original error", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "clusters"), 0o750))

		err := notFoundWithSuggestions(klist.ErrClusterNotFound, dir, "bogus", "acme")
		require.Error(t, err)
		// No "available in" line because there's nothing to suggest.
		assert.NotContains(t, err.Error(), "clusters available")
		assert.Contains(t, err.Error(), "bogus.acme")
	})

	t.Run("ListClusters error falls back to original error", func(t *testing.T) {
		t.Parallel()

		// Registry path that doesn't exist → ListClusters errors → we
		// fall through to the plain wrapped error.
		err := notFoundWithSuggestions(klist.ErrClusterNotFound,
			"/nonexistent/path", "bogus", "acme")
		require.Error(t, err)
		assert.NotContains(t, err.Error(), "clusters available")
	})
}

func TestParsePositional(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in         string
		wantClus   string
		wantCust   string
		wantErrSub string
	}{
		{in: "staging.acme", wantClus: "staging", wantCust: "acme"},
		{in: "atat.empire", wantClus: "atat", wantCust: "empire"},
		// Multi-dot customer half is preserved (matches cert.SplitCN).
		{in: "prod.acme.eu", wantClus: "prod", wantCust: "acme.eu"},
		{in: "no-dot-here", wantErrSub: "no '.'"},
		{in: "", wantErrSub: "no '.'"},
		{in: ".trailing", wantErrSub: "empty cluster"},
		{in: "leading.", wantErrSub: "empty cluster or customer"},
	}

	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			cl, cu, err := parsePositional(tc.in)
			if tc.wantErrSub != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrSub)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantClus, cl)
			assert.Equal(t, tc.wantCust, cu)
		})
	}
}

func TestPlural(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "", plural(1))
	assert.Equal(t, "s", plural(0))
	assert.Equal(t, "s", plural(2))
}

// --------------------------------------------------------------------------
// classifyKubeloginErr
// --------------------------------------------------------------------------

func TestClassifyKubeloginErr(t *testing.T) {
	t.Parallel()

	// stderrText snippets are taken (or shaped to match) what kubelogin
	// actually emits. The exact format may shift across kubelogin
	// releases — these substrings are what we observed against v1.36.1
	// and have been stable for several years.
	tests := []struct {
		name        string
		stderr      string
		wantContain string
	}{
		{
			name:        "DNS no-such-host",
			stderr:      `error: get-token: oidc discovery error: Get "...": dial tcp: lookup foo: no such host`,
			wantContain: "issuer hostname is not resolvable",
		},
		{
			name:        "DNS server misbehaving (the live demo case)",
			stderr:      `oidc discovery error: Get "...": dial tcp: lookup keycloak.demo.example on 127.0.0.53:53: server misbehaving`,
			wantContain: "DNS lookup failed",
		},
		{
			name:        "connection refused",
			stderr:      `oidc discovery error: Get "...": dial tcp 1.2.3.4:443: connect: connection refused`,
			wantContain: "is not listening",
		},
		{
			name:        "timeout",
			stderr:      `oidc discovery error: Get "...": context deadline exceeded`,
			wantContain: "did not respond in time",
		},
		{
			name:        "TLS error",
			stderr:      `oidc discovery error: Get "...": tls: failed to verify certificate: x509: certificate signed by unknown authority`,
			wantContain: "TLS error",
		},
		{
			name:        "discovery error of an unknown shape falls back to generic",
			stderr:      `oidc discovery error: something we have not categorised yet`,
			wantContain: "failed to reach issuer",
		},
		{
			name:        "context canceled mid-flow",
			stderr:      `error: get-token: ... context canceled`,
			wantContain: "cancelled before authentication completed",
		},
		{
			name:        "uncategorised stderr surfaces the first line as a clue",
			stderr:      "some unrelated kubelogin error",
			wantContain: "kubelogin: some unrelated kubelogin error",
		},
		{
			name:        "empty stderr falls back to plain wrap with exit status",
			stderr:      "",
			wantContain: "kubelogin authentication failed",
		},
	}

	upstream := errors.New("exit status 1")

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := classifyKubeloginErr(upstream, tc.stderr)
			require.Error(t, err)
			assert.True(t, strings.Contains(err.Error(), tc.wantContain),
				"want error to contain %q, got %q", tc.wantContain, err.Error())
		})
	}
}
