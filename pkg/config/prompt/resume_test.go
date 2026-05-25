// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package prompt

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-cli/pkg/constants"
)

const (
	testAWSRegion         = "eu-west-1"
	testAWSCPInstanceType = "t3.medium"
)

func TestAskSaveInterruptedConfig(t *testing.T) {
	origReader := interruptedConfigSaveReader
	origWriter := interruptedConfigSaveWriter
	t.Cleanup(func() {
		interruptedConfigSaveReader = origReader
		interruptedConfigSaveWriter = origWriter
	})

	tests := []struct {
		name string
		in   string
		want bool
	}{
		{name: "yes", in: "y\n", want: true},
		{name: "full yes", in: "YES\n", want: true},
		{name: "default no", in: "\n", want: false},
		{name: "explicit no", in: "n\n", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			interruptedConfigSaveReader = bytes.NewBufferString(tc.in)
			interruptedConfigSaveWriter = &bytes.Buffer{}

			got, err := askSaveInterruptedConfig("/tmp/configs")

			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestPromptStateRoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		state *promptState
	}{
		{
			name: "partial workload prompt state",
			state: &promptState{
				K8sProfile:       true,
				Basics:           true,
				WorkloadKeycloak: true,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()

			require.NoError(t, writePromptState(dir, tc.state))

			got, loaded, err := loadPromptState(dir)
			require.NoError(t, err)
			require.True(t, loaded)
			assert.Equal(t, *tc.state, got)

			require.NoError(t, removePromptState(dir))

			_, loaded, err = loadPromptState(dir)
			require.NoError(t, err)
			assert.False(t, loaded)
		})
	}
}

func TestLoadExistingPromptedConfig(t *testing.T) {
	tests := []struct {
		name       string
		fixtureDir string
		want       *PromptedConfig
	}{
		{
			name:       "loads existing azure workload config",
			fixtureDir: "existing_prompted_config",
			want: &PromptedConfig{
				ClusterName:                "demo",
				ClusterType:                "workload",
				K8sVersion:                 "v1.33.0",
				KubePrometheusVersion:      "v0.15.0",
				EnableAuditLogging:         true,
				EnableOIDC:                 true,
				OIDCIssuerURL:              "https://keycloak.example.com/realms/demo",
				OIDCClientID:               "kubernetes-demo",
				KeycloakMode:               "external",
				KeycloakDNS:                "keycloak.example.com",
				KeycloakRealm:              "demo",
				SSHUsername:                "git",
				SSHKeyPath:                 "/tmp/id_ed25519",
				KubeaidForkURL:             "https://github.com/Obmondo/KubeAid.git",
				KubeaidVersion:             "v1.2.3",
				KubeaidConfigForkURL:       "git@example.com:org/config.git",
				KubeaidConfigDir:           "demo",
				KubeaidConfigDeployKeyPath: "/tmp/deploy_key",
				GitKnownHosts:              []string{"git.example ssh-ed25519 AAAA"},
				CloudProvider:              "azure",
				AzureTenantID:              "tenant",
				AzureSubscriptionID:        "subscription",
				AzureLocation:              "westeurope",
				AzureStorageAccount:        "demosa",
				AzureCPVMSize:              "Standard_B2s",
				AzureCPReplicas:            "3",
				AzureCPDiskSizeGB:          "128",
				AzureClientID:              "client",
				AzureClientSecret:          "secret",
				NetBirdBackendClientSecret: "nb-secret",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			copyTestdataConfig(t, dir, tc.fixtureDir, "general.yaml")
			copyTestdataConfig(t, dir, tc.fixtureDir, "secrets.yaml")

			cfg := &PromptedConfig{}
			require.NoError(t, loadExistingPromptedConfig(dir, cfg))

			assert.Equal(t, tc.want, cfg)
		})
	}
}

func TestLoadExistingPromptedConfigHetznerBareMetalTopology(t *testing.T) {
	dir := t.TempDir()
	want := completePromptedConfig(constants.CloudProviderHetzner)
	want.HetznerMode = constants.HetznerModeBareMetal
	want.HetznerSSHKeyName = "bm-key"
	want.HetznerRobotUser = "robot-user"
	want.HetznerRobotPassword = "robot-pass"
	want.HetznerCPReplicas = "3"
	want.HetznerVSwitchName = "bm-vswitch"
	want.HetznerVSwitchVLANID = "4002"
	want.HetznerVSwitchSubnetCIDR = "10.0.1.0/24"
	want.HetznerBMCPServerIDs = []string{"1234567", "1234568", "1234569"}
	want.HetznerBMCPPrivateIPs = []string{"10.0.1.2", "10.0.1.3", "10.0.1.4"}
	want.HetznerBMNodeGroupName = "workers"
	want.HetznerBMNodeGroupServerIDs = []string{"1234570", "1234571"}
	want.HetznerBMNodeGroupPrivateIPs = []string{"10.0.1.10", "10.0.1.11"}
	want.HetznerBMEndpointHost = "203.0.113.10"
	want.HetznerBMEndpointIsFailoverIP = true
	want.HetznerBMServerPublicIPs = map[string]string{
		"1234567": "198.51.100.1",
		"1234568": "198.51.100.2",
		"1234569": "198.51.100.3",
		"1234570": "198.51.100.10",
		"1234571": "198.51.100.11",
	}

	require.NoError(t, writeConfigFiles(dir, want))

	got := &PromptedConfig{}
	require.NoError(t, loadExistingPromptedConfig(dir, got))

	assert.Equal(t, constants.CloudProviderHetzner, got.CloudProvider)
	assert.Equal(t, constants.HetznerModeBareMetal, got.HetznerMode)
	assert.Equal(t, "bm-key", got.HetznerSSHKeyName)
	assert.Equal(t, "robot-user", got.HetznerRobotUser)
	assert.Equal(t, "robot-pass", got.HetznerRobotPassword)
	assert.Equal(t, "3", got.HetznerCPReplicas)
	assert.Equal(t, "bm-vswitch", got.HetznerVSwitchName)
	assert.Equal(t, "4002", got.HetznerVSwitchVLANID)
	assert.Equal(t, "10.0.1.0/24", got.HetznerVSwitchSubnetCIDR)
	assert.Equal(t, want.HetznerBMCPServerIDs, got.HetznerBMCPServerIDs)
	assert.Equal(t, want.HetznerBMCPPrivateIPs, got.HetznerBMCPPrivateIPs)
	assert.Equal(t, "workers", got.HetznerBMNodeGroupName)
	assert.Equal(t, want.HetznerBMNodeGroupServerIDs, got.HetznerBMNodeGroupServerIDs)
	assert.Equal(t, want.HetznerBMNodeGroupPrivateIPs, got.HetznerBMNodeGroupPrivateIPs)
	assert.Equal(t, "203.0.113.10", got.HetznerBMEndpointHost)
	assert.True(t, got.HetznerBMEndpointIsFailoverIP)
	assert.False(t, missingProviderPromptConfig(got))
}

func TestLoadExistingPromptedConfigHetznerHybridTopology(t *testing.T) {
	dir := t.TempDir()
	want := completePromptedConfig(constants.CloudProviderHetzner)
	want.HetznerMode = constants.HetznerModeHybrid
	want.HetznerSSHKeyName = "hybrid-key"
	want.HetznerAPIToken = "hcloud-token"
	want.HetznerRobotUser = "robot-user"
	want.HetznerRobotPassword = "robot-pass"
	want.HetznerHCloudZone = "eu-central"
	want.HetznerCPMachineType = "cax21"
	want.HetznerCPReplicas = "3"
	want.HetznerLBRegion = "hel1"
	want.HetznerRegion = "hel1"
	want.HetznerBMCPRegions = []string{"hel1"}
	want.HetznerVSwitchName = "hybrid-vswitch"
	want.HetznerVSwitchVLANID = "4001"
	want.HetznerVSwitchSubnetCIDR = "10.0.1.0/24"
	want.HetznerBMNodeGroupName = "hybrid-workers"
	want.HetznerBMNodeGroupServerIDs = []string{"1234570", "1234571"}
	want.HetznerBMNodeGroupPrivateIPs = []string{"10.0.1.10", "10.0.1.11"}
	want.HetznerBMServerPublicIPs = map[string]string{
		"1234570": "198.51.100.10",
		"1234571": "198.51.100.11",
	}

	require.NoError(t, writeConfigFiles(dir, want))

	got := &PromptedConfig{}
	require.NoError(t, loadExistingPromptedConfig(dir, got))

	assert.Equal(t, constants.CloudProviderHetzner, got.CloudProvider)
	assert.Equal(t, constants.HetznerModeHybrid, got.HetznerMode)
	assert.Equal(t, "hybrid-key", got.HetznerSSHKeyName)
	assert.Equal(t, "hcloud-token", got.HetznerAPIToken)
	assert.Equal(t, "robot-user", got.HetznerRobotUser)
	assert.Equal(t, "robot-pass", got.HetznerRobotPassword)
	assert.Equal(t, "eu-central", got.HetznerHCloudZone)
	assert.Equal(t, "cax21", got.HetznerCPMachineType)
	assert.Equal(t, "3", got.HetznerCPReplicas)
	assert.Equal(t, "hel1", got.HetznerLBRegion)
	assert.Equal(t, "hel1", got.HetznerRegion)
	assert.Equal(t, []string{"hel1"}, got.HetznerBMCPRegions,
		"resume must mirror controlPlane.regions onto HetznerBMCPRegions so a re-render doesn't drop back to []")
	assert.Equal(t, "hybrid-vswitch", got.HetznerVSwitchName)
	assert.Equal(t, "4001", got.HetznerVSwitchVLANID)
	assert.Equal(t, "10.0.1.0/24", got.HetznerVSwitchSubnetCIDR)
	assert.Equal(t, "hybrid-workers", got.HetznerBMNodeGroupName)
	assert.Equal(t, want.HetznerBMNodeGroupServerIDs, got.HetznerBMNodeGroupServerIDs)
	assert.Equal(t, want.HetznerBMNodeGroupPrivateIPs, got.HetznerBMNodeGroupPrivateIPs)
	assert.False(t, missingProviderPromptConfig(got))
}

func TestAWSProviderPromptStateDoesNotDependOnEnvOrGitSSH(t *testing.T) {
	clearAWSEnvironment(t)

	cfg := completePromptedConfig(constants.CloudProviderAWS)
	cfg.KubeaidConfigDeployKeyPath = ""
	cfg.AWSSSHKeyName = ""
	cfg.AWSRegion = testAWSRegion
	cfg.AWSCPInstanceType = testAWSCPInstanceType
	cfg.AWSCPReplicas = "3"
	cfg.AWSAMIID = "ami-1234567890abcdef0"

	state := completedPromptStateFromValues(cfg)

	assert.True(t, state.ProviderCredentials)
	assert.False(t, state.GitSSH)
	assert.False(t, missingProviderPromptConfig(cfg))
	assert.True(t, missingProviderRenderedConfig(cfg))
}

func TestConfigNeedsInteractiveResumeAWSExternalCredentials(t *testing.T) {
	clearAWSEnvironment(t)
	dir := t.TempDir()

	cfg := completePromptedConfig(constants.CloudProviderAWS)
	cfg.AWSRegion = testAWSRegion
	cfg.AWSSSHKeyName = "deploy-key"
	cfg.AWSCPInstanceType = testAWSCPInstanceType
	cfg.AWSCPReplicas = "3"
	cfg.AWSAMIID = "ami-1234567890abcdef0"

	require.NoError(t, writeConfigFiles(dir, cfg))

	got, err := ConfigNeedsInteractiveResume(dir)

	require.NoError(t, err)
	assert.False(t, got)
}

func TestConfigNeedsInteractiveResume(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, dir string)
		want  bool
	}{
		{
			name: "state file forces resume",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				require.NoError(t, writePromptState(dir, &promptState{Basics: true}))
			},
			want: true,
		},
		{
			name: "complete local config does not resume",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				writeCompleteLocalConfig(t, dir)
			},
			want: false,
		},
		{
			name: "missing cluster name resumes",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				writeCompleteLocalConfig(t, dir)

				generalPath := filepath.Join(dir, "general.yaml")
				data, err := os.ReadFile(generalPath)
				require.NoError(t, err)
				data = bytes.ReplaceAll(data, []byte("  name: demo"), []byte("  name:"))
				//nolint:gosec // generalPath is under t.TempDir with a fixed filename.
				require.NoError(t, os.WriteFile(generalPath, data, 0o600))
			},
			want: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			tc.setup(t, dir)

			got, err := ConfigNeedsInteractiveResume(dir)

			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func writeCompleteLocalConfig(t *testing.T, dir string) {
	t.Helper()
	copyTestdataConfig(t, dir, "complete_local_config", "general.yaml")
	copyTestdataConfig(t, dir, "complete_local_config", "secrets.yaml")
}

func completePromptedConfig(cloudProvider string) *PromptedConfig {
	return &PromptedConfig{
		ClusterName:                "demo",
		ClusterType:                constants.ClusterTypeWorkload,
		K8sVersion:                 "v1.33.0",
		SSHUsername:                "git",
		UseSSHAgent:                true,
		KubeaidForkURL:             "https://github.com/Obmondo/KubeAid.git",
		KubeaidVersion:             "v1.2.3",
		KubeaidConfigForkURL:       "git@example.com:org/config.git",
		KubeaidConfigDir:           "demo",
		KubeaidConfigDeployKeyPath: "/tmp/deploy_key",
		CloudProvider:              cloudProvider,
	}
}

func clearAWSEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv(constants.EnvNameAWSAccessKey, "")
	t.Setenv(constants.EnvNameAWSSecretKey, "")
	t.Setenv(constants.EnvNameAWSSessionToken, "")
	t.Setenv(constants.EnvNameAWSRegion, "")
}

func copyTestdataConfig(t *testing.T, dir, fixtureDir, name string) {
	t.Helper()

	data, err := os.ReadFile(
		filepath.Join("..", "..", "..", "testdata", "config", "prompt", fixtureDir, name),
	)
	require.NoError(t, err)
	//nolint:gosec // dir is always t.TempDir and name is a fixed test fixture filename.
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), data, 0o600))
}
