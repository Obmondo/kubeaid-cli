// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"path"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
)

// TestCrossplaneProviderValuesTemplate covers values-crossplane-provider.yaml.tmpl.
//
// The crossplane-provider chart switches each provider sub-chart and each composition on with
// an `enabled` key. The two charts it replaces used `enable`, which Helm would silently ignore
// here, leaving the whole Azure sub-chart off.
func TestCrossplaneProviderValuesTemplate(t *testing.T) {
	const tmplPath = "templates/argocd-apps/values-crossplane-provider.yaml.tmpl"

	t.Run("no Azure config renders no provider", func(t *testing.T) {
		parsed := renderTemplateToMap(t, tmplPath, forkTV(""))
		assert.Empty(t, parsed)
	})

	t.Run("Azure without disaster recovery", func(t *testing.T) {
		tv := forkTV("")
		tv.AzureConfig = &config.AzureConfig{}

		azure := subMap(t, renderTemplateToMap(t, tmplPath, tv), "azure")
		assert.Equal(t, true, azure["enabled"])
		assert.NotContains(t, azure, "enable")

		compositions := subMap(t, azure, "compositions")
		assert.Equal(t, true,
			subMap(t, compositions, "workloadIdentityInfrastructure")["enabled"])
		assert.NotContains(t, compositions, "disasterRecoveryInfrastructure",
			"the DR composition has no claim to serve without cloud.disasterRecovery")
	})

	t.Run("Azure with disaster recovery", func(t *testing.T) {
		tv := forkTV("")
		tv.AzureConfig = &config.AzureConfig{}
		tv.DisasterRecoveryConfig = &config.DisasterRecoveryConfig{}

		compositions := subMap(t,
			subMap(t, renderTemplateToMap(t, tmplPath, tv), "azure"), "compositions")
		assert.Equal(t, true,
			subMap(t, compositions, "workloadIdentityInfrastructure")["enabled"])
		assert.Equal(t, true,
			subMap(t, compositions, "disasterRecoveryInfrastructure")["enabled"])
	})
}

// TestCrossplaneProviderArgoCDAppTemplate pins the chart path and the values file the
// crossplane-provider ArgoCD App points at, and the name InstallAndSetupCrossplane syncs it by.
func TestCrossplaneProviderArgoCDAppTemplate(t *testing.T) {
	app := renderTemplateToMap(t,
		"templates/argocd-apps/templates/crossplane-provider.yaml.tmpl", forkTV(""))

	assert.Equal(t, constants.ArgoCDAppCrossplaneProvider, subMap(t, app, "metadata")["name"])

	spec := subMap(t, app, "spec")
	assert.Equal(t, constants.NamespaceCrossPlane, subMap(t, spec, "destination")["namespace"])

	sources, ok := spec["sources"].([]any)
	require.True(t, ok, "expected spec.sources to be a list, got %T", spec["sources"])
	require.Len(t, sources, 2)

	chartSource, ok := sources[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "argocd-helm-charts/crossplane-provider", chartSource["path"])
	assert.Equal(t, "master", chartSource["targetRevision"])
	assert.Equal(t,
		[]any{"$values/k8s/demo/argocd-apps/values-crossplane-provider.yaml"},
		subMap(t, chartSource, "helm")["valueFiles"])

	assert.Contains(t, subMap(t, spec, "syncPolicy"), "retry")
}

// TestAzureXRClaimTemplatesAPIVersion pins the claim templates to the API group and version
// Azure.ProvisionInfrastructure polls for readiness. When the two drifted apart, the poll could
// never find the claims and the bootstrap waited on them forever.
func TestAzureXRClaimTemplatesAPIVersion(t *testing.T) {
	tv := forkTV("")
	tv.AzureConfig = &config.AzureConfig{AADApplication: &config.AADApplication{}}

	wantAPIVersion := constants.AzureXRClaimAPIGroup + "/" + constants.AzureXRClaimAPIVersion

	for templateName, wantKind := range map[string]string{
		"workload-identity-infrastructure.yaml.tmpl": "WorkloadIdentityInfrastructure",
		"disaster-recovery-infrastructure.yaml.tmpl": "DisasterRecoveryInfrastructure",
	} {
		claim := renderTemplateToMap(t,
			path.Join("templates/infrastructure/azure", templateName), tv)

		assert.Equal(t, wantAPIVersion, claim["apiVersion"], templateName)
		assert.Equal(t, wantKind, claim["kind"], templateName)
		assert.Equal(t, constants.NamespaceCrossPlane,
			subMap(t, claim, "metadata")["namespace"], templateName)
	}
}

// TestAzureTemplateNamesAreEmbedded guards the Azure template name lists against naming a
// template that isn't shipped : rendering would otherwise only fail in the middle of a bootstrap.
func TestAzureTemplateNamesAreEmbedded(t *testing.T) {
	templateNames := slices.Concat(
		constants.AzureSpecificNonSecretTemplateNames,
		constants.AzureAKSSpecificNonSecretTemplateNames,
		constants.AzureSpecificSecretTemplateNames,
		constants.AzureAKSSpecificSecretTemplateNames,
		constants.AzureDisasterRecoverySpecificNonSecretTemplateNames,
		constants.AzureDisasterRecoverySpecificSecretTemplateNames,
	)

	for _, templateName := range templateNames {
		_, err := KubeaidConfigFileTemplates.ReadFile(path.Join("templates", templateName))
		assert.NoError(t, err, "template %q is listed but not embedded", templateName)
	}

	assert.Contains(t, constants.AzureSpecificNonSecretTemplateNames,
		"argocd-apps/templates/crossplane-provider.yaml.tmpl")
	assert.Contains(t, constants.AzureSpecificNonSecretTemplateNames,
		"argocd-apps/values-crossplane-provider.yaml.tmpl")
}
