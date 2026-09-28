// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package siem

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/siemctl"
)

func testCert(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "sealed-secrets-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	p := filepath.Join(t.TempDir(), "cert.pem")
	require.NoError(t, os.WriteFile(p, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600))
	return p
}

func run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	origGeneral, origSecrets := config.ParsedGeneralConfig, config.ParsedSecretsConfig
	t.Cleanup(func() {
		config.ParsedGeneralConfig = origGeneral
		config.ParsedSecretsConfig = origSecrets
	})
	// Flags keep their values between Execute calls: reset every one.
	resetFlags(SiemCmd)
	var out bytes.Buffer
	SiemCmd.SetOut(&out)
	SiemCmd.SetErr(&out)
	SiemCmd.SetArgs(args)
	err := SiemCmd.Execute()
	return out.String(), err
}

func TestCommandTree(t *testing.T) {
	want := []string{
		"render", "init", "apply", "preflight", "status", "upgrade", "tenant add", "tenant remove",
		"backup", "restore", "enroll", "bundle", "bundle import", "quickstart",
	}
	for _, path := range want {
		cmd, _, err := SiemCmd.Find(splitWords(path))
		require.NoError(t, err, path)
		assert.Equal(t, lastWord(path), cmd.Name(), path)
	}
}

func TestInitTenantAndDryRunApply(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := filepath.Join(t.TempDir(), "k8s", "demo")
	cert := testCert(t)

	out, err := run(t, "init", "--cluster-dir", dir, "--non-interactive",
		"--cluster-name", "demo",
		"--kubeaid-url", "https://github.com/example/KubeAid",
		"--kubeaid-config-url", "https://git.example.com/example/kubeaid-config.git",
		"--domain", "example.com", "--keycloak-url", "https://keycloak.example.com/auth",
		"--agent-host", "agents.example.com", "--agent-address", "192.0.2.10",
		"--reconciler", "--reconciler-dry-run=false",
		"--tenant", "001=Tenant A")
	require.NoError(t, err, out)
	assert.Contains(t, out, "1 tenants")

	f, err := siemctl.OpenGeneralConfig(filepath.Join(dir, generalConfigFileName))
	require.NoError(t, err)
	cfg, err := f.SecurityOperations()
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.True(t, cfg.Reconciler.Enabled)
	assert.False(t, *cfg.Reconciler.DryRun)

	// Missing required fields without a terminal: an error, no prompt.
	_, err = run(t, "init", "--cluster-dir", filepath.Join(t.TempDir(), "x"), "--non-interactive")
	assert.ErrorContains(t, err, "missing")

	out, err = run(t, "tenant", "add", "002", "--name", "Tenant B", "--cluster-dir", dir, "--sealed-secrets-cert", cert)
	require.NoError(t, err, out)
	assert.FileExists(t, filepath.Join(dir, "sealed-secrets", "wazuh-002", "wazuh-authd-pass.yaml"))
	assert.FileExists(t, filepath.Join(dir, "argocd-apps", "templates", "security-operations.yaml"))

	// A dry-run apply: the render is up to date, so no diff.
	out, err = run(t, "apply", "--cluster-dir", dir, "--dry-run", "--skip-preflight", "--sealed-secrets-cert", cert)
	require.NoError(t, err, out)
	assert.Contains(t, out, "No changes")

	// A dry-run tenant add shows the new tenant's files and writes nothing.
	out, err = run(t, "tenant", "add", "003", "--name", "Tenant C", "--cluster-dir", dir, "--sealed-secrets-cert", cert, "--dry-run")
	require.NoError(t, err, out)
	assert.Contains(t, out, "wazuh-003")
	assert.NoDirExists(t, filepath.Join(dir, "sealed-secrets", "wazuh-003"))

	// Removal needs --confirm.
	_, err = run(t, "tenant", "remove", "002", "--cluster-dir", dir)
	assert.ErrorContains(t, err, "--confirm")
	out, err = run(t, "tenant", "remove", "002", "--cluster-dir", dir, "--confirm", "--sealed-secrets-cert", cert)
	require.NoError(t, err, out)
	assert.NoDirExists(t, filepath.Join(dir, "sealed-secrets", "wazuh-002"))
	apps, err := os.ReadFile(filepath.Join(dir, "argocd-apps", "templates", "security-operations.yaml"))
	require.NoError(t, err)
	assert.NotContains(t, string(apps), "wazuh-002")
	assert.Contains(t, string(apps), "wazuh-001")

	// restore refuses without --confirm.
	_, err = run(t, "restore", "kubesoc-x")
	assert.ErrorContains(t, err, "--confirm")
}

func resetFlags(cmd *cobra.Command) {
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		if sv, ok := f.Value.(pflag.SliceValue); ok {
			_ = sv.Replace(nil)
		} else {
			_ = f.Value.Set(f.DefValue)
		}
		f.Changed = false
	})
	for _, c := range cmd.Commands() {
		resetFlags(c)
	}
}

func splitWords(s string) []string {
	var out []string
	for _, w := range bytes.Fields([]byte(s)) {
		out = append(out, string(w))
	}
	return out
}

func lastWord(s string) string {
	w := splitWords(s)
	return w[len(w)-1]
}
