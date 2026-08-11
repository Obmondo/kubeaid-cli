// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-cli/pkg/constants"
)

// securityApp describes one optional security ArgoCD App: the manifest and
// values templates, and the chart + namespace the manifest must point at.
type securityApp struct {
	name       string
	appTmpl    string
	valuesTmpl string
	chartPath  string
	namespace  string
	valuesFile string
}

var securityApps = []securityApp{
	{
		name:       "trivy-operator",
		appTmpl:    "templates/argocd-apps/templates/trivy-operator.yaml.tmpl",
		valuesTmpl: "templates/argocd-apps/values-trivy-operator.yaml.tmpl",
		chartPath:  "argocd-helm-charts/trivy-operator",
		namespace:  "trivy-system",
		valuesFile: "values-trivy-operator.yaml",
	},
	{
		name:       "version-checker",
		appTmpl:    "templates/argocd-apps/templates/version-checker.yaml.tmpl",
		valuesTmpl: "templates/argocd-apps/values-version-checker.yaml.tmpl",
		chartPath:  "argocd-helm-charts/version-checker",
		namespace:  "version-checker",
		valuesFile: "values-version-checker.yaml",
	},
	{
		name:       "tetragon",
		appTmpl:    "templates/argocd-apps/templates/tetragon.yaml.tmpl",
		valuesTmpl: "templates/argocd-apps/values-tetragon.yaml.tmpl",
		chartPath:  "argocd-helm-charts/tetragon",
		namespace:  "tetragon",
		valuesFile: "values-tetragon.yaml",
	},
}

// TestSecurityApps covers the ArgoCD Application manifests for the optional
// security Apps: each must point at its KubeAid chart, land in its own
// namespace, and read its values file out of the kubeaid-config fork.
func TestSecurityApps(t *testing.T) {
	for _, app := range securityApps {
		t.Run(app.name+" Application manifest", func(t *testing.T) {
			manifest := renderTemplateToMap(t, app.appTmpl, forkTV(""))

			assert.Equal(t, "argoproj.io/v1alpha1", manifest["apiVersion"])
			assert.Equal(t, "Application", manifest["kind"])

			metadata := subMap(t, manifest, "metadata")
			assert.Equal(t, app.name, metadata["name"])
			assert.Equal(t, "argocd", metadata["namespace"])

			labels := subMap(t, metadata, "labels")
			assert.Equal(t, "kubeaid", labels["kubeaid.io/managed-by"],
				"the root App selects on this label")
			assert.Equal(t, "master", labels["kubeaid.io/version"],
				"pinned to the KubeAid fork version, not the chart version")

			spec := subMap(t, manifest, "spec")
			assert.Equal(t, "kubeaid", spec["project"])

			destination := subMap(t, spec, "destination")
			assert.Equal(t, app.namespace, destination["namespace"])

			sources, ok := spec["sources"].([]any)
			require.True(t, ok, "spec.sources must be a list")
			require.Len(t, sources, 2,
				"one source for the KubeAid chart, one ref for the values repo")

			chartSource, ok := sources[0].(map[string]any)
			require.True(t, ok)
			assert.Equal(t, app.chartPath, chartSource["path"])
			assert.Equal(t, "https://example.test/KubeAid.git", chartSource["repoURL"])

			helm, ok := chartSource["helm"].(map[string]any)
			require.True(t, ok, "the chart source must carry a helm block")
			valueFiles, ok := helm["valueFiles"].([]any)
			require.True(t, ok)
			require.Len(t, valueFiles, 1)
			assert.Equal(t,
				"$values/k8s/demo/argocd-apps/"+app.valuesFile,
				valueFiles[0],
				"values must resolve through the $values ref into the config fork")

			valuesSource, ok := sources[1].(map[string]any)
			require.True(t, ok)
			assert.Equal(t, "values", valuesSource["ref"])
			assert.Equal(t, "https://example.test/cfg.git", valuesSource["repoURL"])
		})
	}
}

// TestSecurityAppValuesNesting guards the wrapper-chart nesting. The KubeAid
// charts wrap an upstream subchart of the same name, so overrides sit one
// level deep — nesting them twice silently sets values nothing reads.
func TestSecurityAppValuesNesting(t *testing.T) {
	t.Run("trivy-operator overrides sit directly under the subchart key", func(t *testing.T) {
		parsed := renderTemplateToMap(t,
			"templates/argocd-apps/values-trivy-operator.yaml.tmpl", forkTV(""))

		trivy := subMap(t, parsed, "trivy-operator")
		assert.NotContains(t, trivy, "trivy-operator",
			"double nesting would set a value the subchart never reads")
		assert.Contains(t, trivy, "excludeNamespaces")

		// The wrapper's own values, a sibling of the subchart key.
		kubeaid := subMap(t, parsed, "kubeaid")
		prometheusRule := subMap(t, kubeaid, "prometheusRule")
		assert.Equal(t, true, prometheusRule["enabled"])
	})

	t.Run("version-checker overrides sit directly under the subchart key", func(t *testing.T) {
		parsed := renderTemplateToMap(t,
			"templates/argocd-apps/values-version-checker.yaml.tmpl", forkTV(""))

		versionChecker := subMap(t, parsed, "version-checker")
		assert.NotContains(t, versionChecker, "version-checker",
			"double nesting would set a value the subchart never reads")
		assert.Equal(t, true, versionChecker["defaultTestAll"])
	})

	t.Run("tetragon takes chart defaults", func(t *testing.T) {
		parsed := renderTemplateToMap(t,
			"templates/argocd-apps/values-tetragon.yaml.tmpl", forkTV(""))
		assert.Contains(t, parsed, "tetragon")
	})
}

// TestSecurityTemplateNameSets pins the template sets the render path appends.
//
// trivy-operator and version-checker belong to one set deliberately: the
// ImageOutdatedAndVulnerable alert joins a metric from each, so rendering
// trivy-operator alone installs an alert that can never fire.
func TestSecurityTemplateNameSets(t *testing.T) {
	assert.Equal(t, []string{
		"argocd-apps/templates/trivy-operator.yaml.tmpl",
		"argocd-apps/values-trivy-operator.yaml.tmpl",
		"argocd-apps/templates/version-checker.yaml.tmpl",
		"argocd-apps/values-version-checker.yaml.tmpl",
	}, constants.VulnerabilityScanningTemplateNames,
		"version-checker must ship with trivy-operator, never separately")

	assert.Equal(t, []string{
		"argocd-apps/templates/tetragon.yaml.tmpl",
		"argocd-apps/values-tetragon.yaml.tmpl",
	}, constants.RuntimeDetectionTemplateNames)
}
