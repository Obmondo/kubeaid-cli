// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/globals"
)

// TestManagementClusterRootChildResources pins the child Apps the root sync creates on the
// management cluster. An App left out is never created there, and syncing it afterwards waits
// on it forever.
//
// Mutates globals.CloudProviderName and config.ParsedGeneralConfig — sequential only.
func TestManagementClusterRootChildResources(t *testing.T) {
	common := []string{"argocd", "sealed-secrets", "secrets", "cert-manager"}
	clusterAPI := []string{"cluster-api-operator", "capi-cluster"}

	tests := []struct {
		name          string
		cloudProvider string
		azureConfig   *config.AzureConfig
		want          []string
	}{
		{
			name:          "local provisions no CAPI cluster",
			cloudProvider: constants.CloudProviderLocal,
			want:          common,
		},
		{
			name:          "hetzner",
			cloudProvider: constants.CloudProviderHetzner,
			want:          append(append([]string{}, common...), clusterAPI...),
		},
		{
			name:          "self-managed azure runs Crossplane on the management cluster",
			cloudProvider: constants.CloudProviderAzure,
			azureConfig:   &config.AzureConfig{},
			want: append(append(append([]string{}, common...), clusterAPI...),
				"crossplane", "crossplane-provider", "infrastructure"),
		},
		{
			name:          "AKS renders no Crossplane Apps",
			cloudProvider: constants.CloudProviderAzure,
			azureConfig:   &config.AzureConfig{AKS: true},
			want:          append(append([]string{}, common...), clusterAPI...),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			savedCloudProvider := globals.CloudProviderName
			savedConfig := config.ParsedGeneralConfig
			t.Cleanup(func() {
				globals.CloudProviderName = savedCloudProvider
				config.ParsedGeneralConfig = savedConfig
			})

			globals.CloudProviderName = tc.cloudProvider
			config.ParsedGeneralConfig = &config.GeneralConfig{
				Cloud: config.CloudConfig{Azure: tc.azureConfig},
			}

			resources := managementClusterRootChildResources()

			got := make([]string, 0, len(resources))
			for _, resource := range resources {
				assert.Equal(t, "argoproj.io", resource.Group)
				assert.Equal(t, "Application", resource.Kind)
				assert.Equal(t, constants.NamespaceArgoCD, resource.Namespace)
				got = append(got, resource.Name)
			}
			assert.Equal(t, tc.want, got)
		})
	}
}
