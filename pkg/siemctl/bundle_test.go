// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package siemctl

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/config/parser"
	"github.com/Obmondo/kubeaid-cli/pkg/core"
)

const renderedManifest = `---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: wazuh-indexer
spec:
  template:
    spec:
      initContainers:
        - name: init
          image: docker.io/bitnamilegacy/os-shell:12-debian-12-r43
      containers:
        - name: indexer
          image: "wazuh/wazuh-indexer:4.14.3"
        - name: sidecar
          image: mikefarah/yq:4.44.6@sha256:b1d117c609ba990436ad1649299e2f6c378f62cb562caf30b6f2fb6144713422
---
apiVersion: batch/v1
kind: CronJob
metadata:
  name: siem-reconciler
spec:
  jobTemplate:
    spec:
      template:
        spec:
          containers:
            - name: reconciler
              image: 'ghcr.io/obmondo/siem-reconciler:v1.0.0'
            - name: again
              image: "wazuh/wazuh-indexer:4.14.3"
---
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: iris-pg
spec:
  imageName: ghcr.io/cloudnative-pg/postgresql:16.4
---
apiVersion: k8s.mariadb.com/v1alpha1
kind: MariaDB
metadata:
  name: misp-db
spec:
  image: docker-registry1.mariadb.com/library/mariadb:11.4.3
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: values-like
data:
  config.yaml: |
    image: not-parsed-inside-a-string:1
---
apiVersion: example.com/v1
kind: Thing
spec:
  image:
    registry: quay.io
    repository: keycloak/keycloak
    tag: 26.0.0
  other:
    image: ""
`

// Names the bundle tests use in more than one place.
const (
	demoChart   = "argocd-helm-charts/demo"
	appRootName = "root"
	tenantApp   = "wazuh-001"
)

func TestExtractImages(t *testing.T) {
	images, err := ExtractImages(renderedManifest)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"docker-registry1.mariadb.com/library/mariadb:11.4.3",
		"docker.io/bitnamilegacy/os-shell:12-debian-12-r43",
		"ghcr.io/cloudnative-pg/postgresql:16.4",
		"ghcr.io/obmondo/siem-reconciler:v1.0.0",
		"mikefarah/yq:4.44.6@sha256:b1d117c609ba990436ad1649299e2f6c378f62cb562caf30b6f2fb6144713422",
		"quay.io/keycloak/keycloak:26.0.0",
		"wazuh/wazuh-indexer:4.14.3",
	}, images)

	_, err = ExtractImages("a: [unclosed")
	assert.Error(t, err)

	none, err := ExtractImages("")
	require.NoError(t, err)
	assert.Empty(t, none)
}

func TestRewriteRef(t *testing.T) {
	for in, want := range map[string]string{
		"wazuh/wazuh-manager:4.14.3":                 "registry.example.com/kubesoc/wazuh/wazuh-manager:4.14.3",
		"docker.io/valkey/valkey:9.0.2":              "registry.example.com/kubesoc/valkey/valkey:9.0.2",
		"alpine":                                     "registry.example.com/kubesoc/library/alpine:latest",
		"ghcr.io/misp/misp-docker/misp-core:v2.5.44": "registry.example.com/kubesoc/misp/misp-docker/misp-core:v2.5.44",
		"mikefarah/yq@sha256:b1d117c609ba990436ad1649299e2f6c378f62cb562caf30b6f2fb6144713422": "registry.example.com/kubesoc/mikefarah/yq@sha256:b1d117c609ba990436ad1649299e2f6c378f62cb562caf30b6f2fb6144713422",
	} {
		got, err := RewriteRef(in, "registry.example.com/kubesoc/")
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}
	_, err := RewriteRef("UPPER CASE", "r")
	assert.Error(t, err)
}

// TestBundleRoundTrip builds a bundle with fake renders and pulls, then
// imports it with a fake push.
func TestBundleRoundTrip(t *testing.T) {
	kubeaid := t.TempDir()
	chart := filepath.Join(kubeaid, "argocd-helm-charts", "demo")
	require.NoError(t, os.MkdirAll(filepath.Join(chart, "templates"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(chart, "Chart.yaml"), []byte("name: demo\nversion: 1.0.0\n"), 0o600))
	content := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(content, "rules.xml"), []byte("<group/>"), 0o600))
	models := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(models, "manifests", "registry.ollama.ai", "library", "llama3.1"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(models, "manifests", "registry.ollama.ai", "library", "llama3.1", "8b"), []byte("{}"), 0o600))

	pulled := map[string]v1.Image{}
	out := filepath.Join(t.TempDir(), "kubesoc-v1.tar")
	m, err := BuildBundle(context.Background(), BundleOptions{
		Out:        out,
		Version:    "v1",
		KubeaidDir: kubeaid,
		ContentDir: content,
		ModelsDir:  models,
		Releases: []HelmRelease{
			{Name: nsCentral, Chart: demoChart},
			{Name: tenantApp, Chart: demoChart},
		},
		Render: func(_ context.Context, rel HelmRelease) (string, error) {
			return "kind: Pod\nspec:\n  containers:\n    - image: example/" + rel.Name + ":1\n    - image: example/shared:2\n", nil
		},
		Pull: func(ref string, _ *v1.Platform) (v1.Image, error) {
			img, err := random.Image(64, 1)
			pulled[ref] = img
			return img, err
		},
	})
	require.NoError(t, err)
	assert.Len(t, m.Images, 3)
	assert.Equal(t, []string{demoChart}, m.Charts)
	assert.Equal(t, []string{"llama3.1:8b"}, m.Models)
	assert.Equal(t, "content", m.Content)
	assert.Len(t, pulled, 3)
	for _, img := range m.Images {
		assert.True(t, strings.HasPrefix(img.Digest, "sha256:"))
	}

	pushed := map[string]v1.Image{}
	extract := t.TempDir()
	mapping, m2, err := ImportBundle(context.Background(), ImportOptions{
		Bundle:    out,
		Registry:  "registry.example.com/kubesoc",
		ExtractTo: extract,
		Push: func(img v1.Image, dst string) error {
			pushed[dst] = img
			return nil
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "v1", m2.Version)
	assert.Equal(t, "registry.example.com/kubesoc/example/shared:2", mapping["example/shared:2"])
	assert.Len(t, pushed, 3)
	src, _ := pulled["example/shared:2"].Digest()
	dst, _ := pushed["registry.example.com/kubesoc/example/shared:2"].Digest()
	assert.Equal(t, src, dst)
	assert.FileExists(t, filepath.Join(extract, "charts", "argocd-helm-charts", "demo", "Chart.yaml"))
	assert.FileExists(t, filepath.Join(extract, "content", "rules.xml"))

	// A manifest-only bundle.
	out2 := filepath.Join(t.TempDir(), "b.tar")
	m3, err := BuildBundle(context.Background(), BundleOptions{
		Out: out2, KubeaidDir: kubeaid, SkipImages: true, ExtraImages: []string{"c/d:2"},
		Releases: []HelmRelease{{Name: "x", Chart: demoChart}},
		Render:   func(context.Context, HelmRelease) (string, error) { return "image: a/b:1\n", nil },
	})
	require.NoError(t, err)
	assert.Equal(t, []BundleImage{{Ref: "a/b:1"}, {Ref: "c/d:2"}}, m3.Images)
}

// TestQuickstartRender renders the quickstart config and the install script.
// With KUBESOC_KUBEAID_DIR pointing at a KubeAid checkout it also renders
// every release with the real charts and extracts their images.
func TestQuickstartRender(t *testing.T) {
	withSealingCert(t)
	origGeneral, origSecrets := config.ParsedGeneralConfig, config.ParsedSecretsConfig
	t.Cleanup(func() {
		config.ParsedGeneralConfig = origGeneral
		config.ParsedSecretsConfig = origSecrets
	})

	kubeaid := os.Getenv("KUBESOC_KUBEAID_DIR")
	realCharts := kubeaid != ""
	if !realCharts {
		kubeaid = t.TempDir()
	}
	dir := t.TempDir()
	clusterDir, path, err := WriteQuickstartConfig(QuickstartOptions{
		Dir:           dir,
		Domain:        "192.0.2.10.nip.io",
		AgentAddress:  "192.0.2.10",
		KeycloakURL:   "http://keycloak.192.0.2.10.nip.io/auth",
		ClusterIssuer: "kubesoc-selfsigned",
		ReconcilerTag: "v1.0.0",
	})
	require.NoError(t, err)
	require.NoError(t, parser.LoadSecurityOperationsConfig(path))
	cfg := config.ParsedGeneralConfig.Cluster.SecurityOperations
	assert.False(t, *cfg.Reconciler.DryRun)
	require.Len(t, cfg.Tenants, 1)
	assert.Equal(t, "demo", cfg.Tenants[0].Code)

	written, err := core.RenderSecurityOperations(context.Background(), clusterDir)
	require.NoError(t, err)
	apps, err := ReadApplications(clusterDir, written)
	require.NoError(t, err)
	order := SyncOrder(apps)
	assert.Equal(t, []string{appRootName, nsCentral, sealedSecretsName, "wazuh-demo"}, order)

	releases, err := HelmReleasesFromApps(apps, kubeaid, clusterDir)
	require.NoError(t, err)
	script, files, err := QuickstartScript(releases, order, clusterDir, kubeaid, filepath.Join(dir, "helm"))
	require.NoError(t, err)
	assert.Contains(t, files, "wazuh-demo.values.yaml")
	so := strings.Index(script, "helm upgrade --install security-operations")
	sec := strings.Index(script, "sealed-secrets/wazuh-demo")
	wz := strings.Index(script, "helm upgrade --install wazuh-demo")
	require.True(t, so > 0 && sec > so && wz > sec, script)
	for _, f := range releases[0].ValueFiles {
		assert.FileExists(t, f, "value files resolve into the rendered cluster directory")
	}
	// A second run keeps the config.
	_, _, err = WriteQuickstartConfig(QuickstartOptions{Dir: dir})
	require.NoError(t, err)

	if !realCharts {
		return
	}
	assert.Contains(t, script, "values-single-"+nsCentral+".yaml")
	for _, rel := range releases {
		manifest, err := RenderRelease(context.Background(), rel)
		require.NoError(t, err, rel.Name)
		images, err := ExtractImages(manifest)
		require.NoError(t, err)
		assert.NotEmpty(t, images, rel.Name)
		t.Logf("%s: %v", rel.Name, images)
	}
}
