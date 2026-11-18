// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-cli/pkg/constants"
)

const fixtureClusterName = "cluster1"

func TestNew_RejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name    string
		general *GeneralConfig
		secrets *SecretsConfig
		wantErr string
	}{
		{
			name:    "returns an error when general is nil",
			general: nil,
			secrets: &SecretsConfig{},
			wantErr: "general config is nil",
		},
		{
			name:    "returns an error when secrets is nil",
			general: &GeneralConfig{Cloud: CloudConfig{AWS: &AWSConfig{}}},
			secrets: nil,
			wantErr: "secrets config is nil",
		},
		{
			name:    "reports general first when both are nil",
			general: nil,
			secrets: nil,
			wantErr: "general config is nil",
		},
		{
			name:    "returns an error when no cloud block is set",
			general: &GeneralConfig{Cluster: ClusterConfig{Name: fixtureClusterName}},
			secrets: &SecretsConfig{},
			wantErr: "no cloud-provider specific details provided",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := New(tc.general, tc.secrets)

			require.ErrorContains(t, err, tc.wantErr)
			assert.Nil(t, cfg)
		})
	}
}

func TestNew_DerivesCloudProvider(t *testing.T) {
	tests := []struct {
		name         string
		cloud        CloudConfig
		wantProvider string
	}{
		{
			name:         "derives aws from the AWS block",
			cloud:        CloudConfig{AWS: &AWSConfig{}},
			wantProvider: constants.CloudProviderAWS,
		},
		{
			name:         "derives azure from the Azure block",
			cloud:        CloudConfig{Azure: &AzureConfig{}},
			wantProvider: constants.CloudProviderAzure,
		},
		{
			name:         "derives hetzner from the Hetzner block",
			cloud:        CloudConfig{Hetzner: &HetznerConfig{}},
			wantProvider: constants.CloudProviderHetzner,
		},
		{
			name:         "derives bare-metal from the BareMetal block",
			cloud:        CloudConfig{BareMetal: &BareMetalConfig{}},
			wantProvider: constants.CloudProviderBareMetal,
		},
		{
			name:         "derives local from the Local block",
			cloud:        CloudConfig{Local: &LocalConfig{}},
			wantProvider: constants.CloudProviderLocal,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			general := &GeneralConfig{
				Cluster: ClusterConfig{Name: fixtureClusterName},
				Cloud:   tc.cloud,
			}
			secrets := &SecretsConfig{}

			cfg, err := New(general, secrets)
			require.NoError(t, err)
			require.NotNil(t, cfg)

			assert.Equal(t, tc.wantProvider, cfg.CloudProviderName)
			assert.Same(t, general, cfg.General, "General must be used in place, not copied")
			assert.Same(t, secrets, cfg.Secrets, "Secrets must be used in place, not copied")
			assert.Empty(t, cfg.CAPIClusterNamespace)
			assert.Zero(t, cfg.Provisioned)
		})
	}
}

// TestNew_UsesGeneralAndSecretsInPlace checks the contract library code relies on: it hydrates
// General and Secrets through the Config, and the caller reads the result from the structs it
// passed in.
func TestNew_UsesGeneralAndSecretsInPlace(t *testing.T) {
	general := &GeneralConfig{
		Cloud: CloudConfig{
			Hetzner: &HetznerConfig{SSHKeyPair: HetznerSSHKeyPair{Name: "cluster1-key"}},
		},
	}
	secrets := &SecretsConfig{}

	cfg, err := New(general, secrets)
	require.NoError(t, err)

	// Writes through the Config reach the caller's structs. Cluster.Name is a plain value field and
	// Secrets.Hetzner is assigned, so a shallow copy of either struct would hide these writes.
	cfg.General.Cluster.Name = fixtureClusterName
	cfg.General.Cloud.Hetzner.SSHKeyPair.Name = "operator-key"
	cfg.Secrets.Hetzner = &HetznerCredentials{APIToken: "demo-token"}

	assert.Equal(t, fixtureClusterName, general.Cluster.Name)
	assert.Equal(t, "operator-key", general.Cloud.Hetzner.SSHKeyPair.Name)
	require.NotNil(t, secrets.Hetzner)
	assert.Equal(t, "demo-token", secrets.Hetzner.APIToken)

	// Edits the caller makes afterwards are visible through the Config.
	general.Cluster.K8sVersion = "1.33.0"
	assert.Equal(t, "1.33.0", cfg.General.Cluster.K8sVersion)
}

func TestConfig_CAPINamespace(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		want      string
	}{
		{
			name:      "returns the default namespace when none is set",
			namespace: "",
			want:      constants.DefaultCAPIClusterNamespace,
		},
		{
			name:      "returns the configured namespace as is",
			namespace: "capi-cluster-democust01",
			want:      "capi-cluster-democust01",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &Config{CAPIClusterNamespace: tc.namespace}

			assert.Equal(t, tc.want, cfg.CAPINamespace())
			assert.Equal(t, tc.namespace, cfg.CAPIClusterNamespace, "CAPINamespace must not write the default back into the field")
		})
	}
}

func TestConfig_CAPINamespaceOfANilConfig(t *testing.T) {
	var cfg *Config

	assert.Equal(t, constants.DefaultCAPIClusterNamespace, cfg.CAPINamespace())
}

// TestCloudConfig_ProviderName covers each provider block, the empty block, and the precedence
// when several blocks are set: ProviderName tests them in the order AWS, Azure, Hetzner,
// BareMetal, Local, and the first one that is set wins.
func TestCloudConfig_ProviderName(t *testing.T) {
	tests := []struct {
		name  string
		cloud CloudConfig
		want  string
	}{
		{
			name:  "returns empty when no block is set",
			cloud: CloudConfig{},
			want:  "",
		},
		{
			name:  "returns empty when only the disaster recovery block is set",
			cloud: CloudConfig{DisasterRecovery: &DisasterRecoveryConfig{}},
			want:  "",
		},
		{
			name:  "returns aws for the AWS block",
			cloud: CloudConfig{AWS: &AWSConfig{}},
			want:  constants.CloudProviderAWS,
		},
		{
			name:  "returns azure for the Azure block",
			cloud: CloudConfig{Azure: &AzureConfig{}},
			want:  constants.CloudProviderAzure,
		},
		{
			name:  "returns hetzner for the Hetzner block",
			cloud: CloudConfig{Hetzner: &HetznerConfig{}},
			want:  constants.CloudProviderHetzner,
		},
		{
			name:  "returns bare-metal for the BareMetal block",
			cloud: CloudConfig{BareMetal: &BareMetalConfig{}},
			want:  constants.CloudProviderBareMetal,
		},
		{
			name:  "returns local for the Local block",
			cloud: CloudConfig{Local: &LocalConfig{}},
			want:  constants.CloudProviderLocal,
		},
		{
			name:  "prefers aws over azure",
			cloud: CloudConfig{AWS: &AWSConfig{}, Azure: &AzureConfig{}},
			want:  constants.CloudProviderAWS,
		},
		{
			name:  "prefers azure over hetzner",
			cloud: CloudConfig{Azure: &AzureConfig{}, Hetzner: &HetznerConfig{}},
			want:  constants.CloudProviderAzure,
		},
		{
			name:  "prefers hetzner over bare-metal",
			cloud: CloudConfig{Hetzner: &HetznerConfig{}, BareMetal: &BareMetalConfig{}},
			want:  constants.CloudProviderHetzner,
		},
		{
			name:  "prefers bare-metal over local",
			cloud: CloudConfig{BareMetal: &BareMetalConfig{}, Local: &LocalConfig{}},
			want:  constants.CloudProviderBareMetal,
		},
		{
			name: "prefers aws when every block is set",
			cloud: CloudConfig{
				AWS:       &AWSConfig{},
				Azure:     &AzureConfig{},
				Hetzner:   &HetznerConfig{},
				BareMetal: &BareMetalConfig{},
				Local:     &LocalConfig{},
			},
			want: constants.CloudProviderAWS,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.cloud.ProviderName())
		})
	}
}
