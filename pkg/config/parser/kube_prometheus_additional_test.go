// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package parser

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
)

func TestValidateKubePrometheusVersion_Errors(t *testing.T) {
	tests := []struct {
		name       string
		kpVersion  string
		k8sVersion string
		wantErrSub string
	}{
		{
			name:       "incompatible KubePrometheus version",
			kpVersion:  "v0.18.0",
			k8sVersion: "v1.34.2",
			wantErrSub: "aren't officially compatible",
		},
		{
			name:       "missing v prefix",
			kpVersion:  "0.16.0",
			k8sVersion: "v1.34.0",
			wantErrSub: "KubePrometheus version must start with 'v'",
		},
		{
			name:       "malformed semver",
			kpVersion:  "vinvalid",
			k8sVersion: "v1.34.0",
			wantErrSub: "parsing KubePrometheus semantic version",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			origGeneral := config.ParsedGeneralConfig
			origCloudProvider := globals.CloudProviderName
			t.Cleanup(func() {
				config.ParsedGeneralConfig = origGeneral
				globals.CloudProviderName = origCloudProvider
			})
			config.ParsedGeneralConfig = &config.GeneralConfig{
				Cluster: config.ClusterConfig{K8sVersion: tc.k8sVersion},
			}
			globals.CloudProviderName = constants.CloudProviderLocal

			err := validateKubePrometheusVersion(tc.kpVersion, tc.k8sVersion)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErrSub)
		})
	}
}

func TestHydrateKubePrometheusVersion_Errors(t *testing.T) {
	tests := []struct {
		name       string
		k8sVersion string
		wantErrSub string
	}{
		{
			name:       "K8s version outside compatibility matrix",
			k8sVersion: "v1.36.0",
			wantErrSub: "unsupported Kubernetes version",
		},
		{
			name:       "malformed K8s version",
			k8sVersion: "not-a-version",
			wantErrSub: "parsing Kubernetes semantic version",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			origGeneral := config.ParsedGeneralConfig
			t.Cleanup(func() { config.ParsedGeneralConfig = origGeneral })

			config.ParsedGeneralConfig = &config.GeneralConfig{
				Cluster:        config.ClusterConfig{K8sVersion: tc.k8sVersion},
				KubePrometheus: &config.KubePrometheusConfig{},
			}

			err := hydrateKubePrometheusVersion(context.Background())
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErrSub)
		})
	}
}
