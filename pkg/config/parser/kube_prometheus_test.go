// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package parser

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
)

func TestHydrateKubePrometheusVersion(t *testing.T) {
	tests := []struct {
		name              string
		k8sVersion        string
		existingKPVersion string
		expectedKPVersion string
	}{
		{
			name:              "selects default KubePrometheus version for v1.32",
			k8sVersion:        "v1.32.0",
			existingKPVersion: "",
			expectedKPVersion: "v0.16.0",
		},
		{
			name:              "selects latest KubePrometheus version for v1.33",
			k8sVersion:        "v1.33.0",
			existingKPVersion: "",
			expectedKPVersion: "v0.17.0",
		},
		{
			name:              "selects latest KubePrometheus version for v1.34",
			k8sVersion:        "v1.34.0",
			existingKPVersion: "",
			expectedKPVersion: "v0.17.0",
		},
		{
			name:              "selects default KubePrometheus version for v1.35",
			k8sVersion:        "v1.35.0",
			existingKPVersion: "",
			expectedKPVersion: "v0.17.0",
		},
		{
			name:              "does not override existing KubePrometheus version",
			k8sVersion:        "v1.32.0",
			existingKPVersion: "v0.15.0",
			expectedKPVersion: "v0.15.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			originalConfig := config.ParsedGeneralConfig
			t.Cleanup(func() {
				config.ParsedGeneralConfig = originalConfig
			})

			config.ParsedGeneralConfig = &config.GeneralConfig{
				Cluster: config.ClusterConfig{
					K8sVersion: tt.k8sVersion,
				},
				KubePrometheus: &config.KubePrometheusConfig{
					Version: tt.existingKPVersion,
				},
			}

			hydrateKubePrometheusVersion(ctx)
			assert.Equal(t, tt.expectedKPVersion, config.ParsedGeneralConfig.KubePrometheus.Version)
		})
	}
}

func TestValidateKubePrometheusVersion_CompatibleCases(t *testing.T) {
	tests := []struct {
		name       string
		kpVersion  string
		k8sVersion string
	}{
		{
			name:       "valid compatible versions - exact match v1.32",
			kpVersion:  "v0.16.0",
			k8sVersion: "v1.32.0",
		},
		{
			name:       "valid compatible versions - v1.33 with v0.16.0",
			kpVersion:  "v0.16.0",
			k8sVersion: "v1.33.0",
		},
		{
			name:       "valid compatible versions - v1.33 with v0.17.0",
			kpVersion:  "v0.17.0",
			k8sVersion: "v1.33.0",
		},
		{
			name:       "valid compatible versions - v1.34 with v0.16.0",
			kpVersion:  "v0.16.0",
			k8sVersion: "v1.34.0",
		},
		{
			name:       "valid compatible versions - v1.34 with v0.17.0",
			kpVersion:  "v0.17.0",
			k8sVersion: "v1.34.0",
		},
		{
			name:       "valid compatible versions - v1.34 with patch bump v0.17.1",
			kpVersion:  "v0.17.1",
			k8sVersion: "v1.34.0",
		},
		{
			name:       "valid compatible versions - v1.35 with v0.17.0",
			kpVersion:  "v0.17.0",
			k8sVersion: "v1.35.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			originalConfig := config.ParsedGeneralConfig
			originalCloudProvider := globals.CloudProviderName
			t.Cleanup(func() {
				config.ParsedGeneralConfig = originalConfig
				globals.CloudProviderName = originalCloudProvider
			})

			config.ParsedGeneralConfig = &config.GeneralConfig{
				Cluster: config.ClusterConfig{
					K8sVersion: tt.k8sVersion,
				},
			}
			globals.CloudProviderName = constants.CloudProviderLocal

			validateKubePrometheusVersion(ctx, tt.kpVersion, tt.k8sVersion)
		})
	}
}
