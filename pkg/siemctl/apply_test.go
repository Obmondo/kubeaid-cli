// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package siemctl

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Names the apply tests use in more than one place.
const (
	valuesFile = "argocd-apps/values-x.yaml"
	appRoot    = "root"
)

const renderedApps = `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  labels:
    kubeaid.io/sync-order: "61"
  name: wazuh-002
  namespace: argocd
spec:
  destination:
    namespace: wazuh-002
  sources:
    - repoURL: https://github.com/example/KubeAid
      path: argocd-helm-charts/kubesoc/charts/wazuh
      targetRevision: v1
      helm:
        valueFiles:
          - $values/k8s/demo/argocd-apps/values-wazuh-tenant.yaml
        valuesObject:
          agentService:
            registrationPort: 20025
    - repoURL: https://git.example.com/kubeaid-config.git
      ref: values
---
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  labels:
    kubeaid.io/sync-order: "60"
  name: security-operations
spec:
  destination:
    namespace: security-operations
  sources:
    - repoURL: https://github.com/example/KubeAid
      path: argocd-helm-charts/kubesoc
      helm:
        valueFiles:
          - $values/k8s/demo/argocd-apps/values-security-operations.yaml
    - repoURL: https://git.example.com/kubeaid-config.git
      ref: values
---
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  labels:
    kubeaid.io/sync-order: "61"
  name: wazuh-001
spec:
  destination:
    namespace: wazuh-001
  sources:
    - path: argocd-helm-charts/kubesoc/charts/wazuh
`

func writeApps(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "k8s", "demo")
	p := filepath.Join(dir, "argocd-apps", "templates", "security-operations.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o700))
	require.NoError(t, os.WriteFile(p, []byte(renderedApps), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "argocd-apps", "values-x.yaml"), []byte("a: 1\n"), 0o600))
	return dir
}

func TestReadApplicationsAndSyncOrder(t *testing.T) {
	dir := writeApps(t)
	apps, err := ReadApplications(dir, []string{
		"argocd-apps/templates/security-operations.yaml",
		valuesFile,
		"sealed-secrets/ignored.txt",
	})
	require.NoError(t, err)
	require.Len(t, apps, 3)
	assert.Equal(t, "wazuh-002", apps[0].Name)
	assert.Equal(t, 61, apps[0].Order)
	assert.Equal(t, "wazuh-002", apps[0].Namespace)
	require.Len(t, apps[0].Sources, 2)
	assert.Equal(t, "argocd-helm-charts/kubesoc/charts/wazuh", apps[0].Sources[0].Path)
	agentService, ok := apps[0].Sources[0].ValuesObject["agentService"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, 20025, agentService["registrationPort"])

	assert.Equal(t,
		[]string{appRoot, nsCentral, sealedSecretsName, nsTenant1, "wazuh-002"},
		SyncOrder(apps))
	assert.Equal(t, []string{appRoot, sealedSecretsName}, SyncOrder(nil))
}

func TestHelmReleasesFromApps(t *testing.T) {
	dir := writeApps(t)
	apps, err := ReadApplications(dir, []string{"argocd-apps/templates/security-operations.yaml"})
	require.NoError(t, err)
	rels, err := HelmReleasesFromApps(apps[:2], "/src/KubeAid", dir)
	require.NoError(t, err)
	configRoot := filepath.Dir(filepath.Dir(dir))
	assert.Equal(t, "/src/KubeAid/argocd-helm-charts/kubesoc/charts/wazuh", rels[0].ChartPath)
	assert.Equal(t, []string{filepath.Join(configRoot, "k8s/demo/argocd-apps/values-wazuh-tenant.yaml")}, rels[0].ValueFiles)
	assert.NotEmpty(t, rels[0].ValuesObject)
	assert.Equal(t, "security-operations", rels[1].Namespace)
}

func TestDryRunDiff(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := writeApps(t)
	render := func(_ context.Context, d string) ([]string, error) {
		return []string{valuesFile}, os.WriteFile(filepath.Join(d, "argocd-apps", "values-x.yaml"), []byte("a: 2\n"), 0o600)
	}
	diff, written, err := DryRunDiff(context.Background(), dir, render)
	require.NoError(t, err)
	assert.Equal(t, []string{valuesFile}, written)
	assert.Contains(t, diff, "-a: 1")
	assert.Contains(t, diff, "+a: 2")
	assert.Contains(t, diff, "a/argocd-apps/values-x.yaml")
	// The cluster directory itself is untouched.
	raw, err := os.ReadFile(filepath.Join(dir, "argocd-apps", "values-x.yaml"))
	require.NoError(t, err)
	assert.Equal(t, "a: 1\n", string(raw))

	same, _, err := DryRunDiff(context.Background(), dir, func(context.Context, string) ([]string, error) { return nil, nil })
	require.NoError(t, err)
	assert.Empty(t, same)
}

func TestGitCommitOnBranch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	ctx := context.Background()
	repo := t.TempDir()
	run := func(args ...string) {
		//nolint:gosec // G204: git with the fixed arguments of this test.
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
	}
	run("init", "-q", "-b", "main")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "test")
	run("config", "commit.gpgsign", "false")
	require.NoError(t, os.MkdirAll(filepath.Join(repo, "k8s", "demo"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(repo, "k8s", "demo", "a.yaml"), []byte("a: 1\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(repo, "other.txt"), []byte("x\n"), 0o600))
	run("add", ".")
	run("commit", "-q", "-m", "init")

	g := Git{Dir: repo}
	branch := DefaultBranchName("apply", time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	assert.Equal(t, "kubesoc/apply-20260102-030405", branch)
	require.NoError(t, g.CheckoutBranch(ctx, branch))
	cur, err := g.CurrentBranch(ctx)
	require.NoError(t, err)
	assert.Equal(t, branch, cur)

	// Nothing changed: no commit.
	sha, err := g.Commit(ctx, "chore: nothing", "k8s/demo")
	require.NoError(t, err)
	assert.Empty(t, sha)

	require.NoError(t, os.WriteFile(filepath.Join(repo, "k8s", "demo", "a.yaml"), []byte("a: 2\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(repo, "other.txt"), []byte("y\n"), 0o600))
	dirty, err := g.Dirty(ctx, "k8s/demo")
	require.NoError(t, err)
	assert.Contains(t, dirty, "k8s/demo/a.yaml")
	assert.NotContains(t, dirty, "other.txt")

	sha, err = g.Commit(ctx, "chore: render", "k8s/demo")
	require.NoError(t, err)
	assert.Len(t, sha, 40)
	// Only the cluster directory was committed.
	dirty, err = g.Dirty(ctx)
	require.NoError(t, err)
	assert.Contains(t, dirty, "other.txt")
	assert.NotContains(t, dirty, "a.yaml")
}

func TestCompareURL(t *testing.T) {
	assert.Equal(t, "https://github.com/example/cfg/compare/main...kubesoc/x",
		CompareURL("git@github.com:example/cfg.git", "main", "kubesoc/x"))
	assert.Equal(t, "https://git.example.com/org/cfg/compare/main...b",
		CompareURL("ssh://git@git.example.com:2222/org/cfg.git", "main", "b"))
	assert.Equal(t, "https://git.example.com/org/cfg/compare/main...b",
		CompareURL("https://git.example.com/org/cfg", "main", "b"))
	assert.Contains(t, CompareURL("https://gitlab.example.com/org/cfg.git", "main", "b"), "/-/merge_requests/new")
	assert.Empty(t, CompareURL("/local/path", "main", "b"))
}
