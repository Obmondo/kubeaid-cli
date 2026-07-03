// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package prompt

import "github.com/Obmondo/kubeaid-cli/pkg/constants"

type promptState struct {
	K8sProfile   bool `yaml:"k8sProfile"`
	Basics       bool `yaml:"basics"`
	VPNKeycloak  bool `yaml:"vpnKeycloak"`
	VPNEndpoints bool `yaml:"vpnEndpoints"`
	// WorkloadLockdown gates the workload Host Firewall (CCNP) + NetBird
	// collection step.
	WorkloadLockdown    bool `yaml:"workloadLockdown"`
	ProviderCredentials bool `yaml:"providerCredentials"`
	GitSSH              bool `yaml:"gitSSH"`
	ObmondoSupport      bool `yaml:"obmondoSupport"`
	NetBirdDNSZone      bool `yaml:"netbirdDNSZone"`
}

func missingBasics(cfg *PromptedConfig) bool {
	return cfg.CloudProvider == "" ||
		cfg.ClusterName == "" ||
		cfg.ClusterType == ""
}

func missingVPNKeycloak(cfg *PromptedConfig) bool {
	if cfg.KeycloakMode == "" || cfg.KeycloakDNS == "" {
		return true
	}
	return cfg.KeycloakMode == constants.KeycloakModeExternal && cfg.NetBirdBackendClientSecret == ""
}

func missingVPNEndpoints(cfg *PromptedConfig) bool {
	return cfg.NetBirdDNS == "" || cfg.ControlPlaneEndpoint == "" || cfg.ACMEEmail == ""
}

func missingProviderPromptConfig(cfg *PromptedConfig) bool {
	switch cfg.CloudProvider {
	case constants.CloudProviderAWS:
		return missingAWSPromptConfig(cfg)
	case constants.CloudProviderAzure:
		return missingAzureCredentials(cfg)
	case constants.CloudProviderHetzner:
		return missingHetznerCredentials(cfg)
	case constants.CloudProviderBareMetal:
		return cfg.BareMetalSSHPort == "" ||
			cfg.BareMetalEndpointHost == "" ||
			cfg.BareMetalEndpointPort == ""
	case constants.CloudProviderLocal:
		return false
	default:
		return true
	}
}

func missingAWSPromptConfig(cfg *PromptedConfig) bool {
	return cfg.AWSRegion == "" ||
		cfg.AWSCPInstanceType == "" ||
		cfg.AWSCPReplicas == "" ||
		cfg.AWSAMIID == ""
}

func missingAzureCredentials(cfg *PromptedConfig) bool {
	return cfg.AzureTenantID == "" ||
		cfg.AzureSubscriptionID == "" ||
		cfg.AzureLocation == "" ||
		cfg.AzureStorageAccount == "" ||
		cfg.AzureCPVMSize == "" ||
		cfg.AzureCPReplicas == "" ||
		cfg.AzureCPDiskSizeGB == "" ||
		cfg.AzureClientID == "" ||
		cfg.AzureClientSecret == ""
}

func missingHetznerCredentials(cfg *PromptedConfig) bool {
	if cfg.HetznerMode == "" {
		return true
	}
	// API token is required for every mode (CAPH validates it on
	// controller startup before bare-metal reconcilers can run).
	if cfg.HetznerAPIToken == "" {
		return true
	}
	if !cfg.UseSSHAgent && cfg.HetznerSSHKeyPath == "" {
		return true
	}
	if missingHetznerRobotCredentials(cfg) {
		return true
	}
	if hetznerUsesHCloud(cfg.HetznerMode) && missingHetznerHCloudConfig(cfg) {
		return true
	}
	return hetznerUsesBareMetal(cfg.HetznerMode) && missingHetznerBareMetalConfig(cfg)
}

func missingHetznerRobotCredentials(cfg *PromptedConfig) bool {
	if cfg.HetznerMode != constants.HetznerModeBareMetal &&
		cfg.HetznerMode != constants.HetznerModeHybrid {
		return false
	}
	return cfg.HetznerRobotUser == "" || cfg.HetznerRobotPassword == ""
}

func hetznerUsesHCloud(mode string) bool {
	return mode == constants.HetznerModeHCloud || mode == constants.HetznerModeHybrid
}

func hetznerUsesBareMetal(mode string) bool {
	return mode == constants.HetznerModeBareMetal || mode == constants.HetznerModeHybrid
}

func missingHetznerHCloudConfig(cfg *PromptedConfig) bool {
	return cfg.HetznerHCloudZone == "" ||
		cfg.HetznerCPMachineType == "" ||
		cfg.HetznerCPReplicas == "" ||
		cfg.HetznerLBRegion == "" ||
		cfg.HetznerRegion == ""
}

func missingHetznerBareMetalConfig(cfg *PromptedConfig) bool {
	if missingHetznerVSwitchConfig(cfg) || missingHetznerBareMetalWorkerConfig(cfg) {
		return true
	}
	if cfg.HetznerMode == constants.HetznerModeBareMetal {
		return missingHetznerBareMetalControlPlaneConfig(cfg)
	}
	return false
}

func missingHetznerVSwitchConfig(cfg *PromptedConfig) bool {
	return cfg.HetznerVSwitchName == "" ||
		cfg.HetznerVSwitchVLANID == "" ||
		cfg.HetznerVSwitchSubnetCIDR == ""
}

func missingHetznerBareMetalControlPlaneConfig(cfg *PromptedConfig) bool {
	return cfg.HetznerCPReplicas == "" ||
		cfg.HetznerBMEndpointHost == "" ||
		missingHetznerBareMetalHosts(cfg.HetznerBMCPServerIDs, cfg.HetznerBMCPPrivateIPs)
}

func missingHetznerBareMetalWorkerConfig(cfg *PromptedConfig) bool {
	return cfg.HetznerBMNodeGroupName == "" ||
		missingHetznerBareMetalHosts(
			cfg.HetznerBMNodeGroupServerIDs,
			cfg.HetznerBMNodeGroupPrivateIPs,
		)
}

func missingHetznerBareMetalHosts(serverIDs []string, privateIPs []string) bool {
	if len(serverIDs) == 0 || len(serverIDs) != len(privateIPs) {
		return true
	}
	for i, serverID := range serverIDs {
		if serverID == "" || privateIPs[i] == "" {
			return true
		}
	}
	return false
}

func missingGitSSH(cfg *PromptedConfig) bool {
	if cfg.KubeaidForkURL == "" ||
		cfg.KubeaidConfigForkURL == "" ||
		cfg.KubeaidConfigDeployKeyPath == "" {
		return true
	}
	if cfg.CloudProvider != constants.CloudProviderLocal && cfg.KubeaidVersion == "" {
		return true
	}
	return !cfg.UseSSHAgent && cfg.SSHKeyPath == ""
}

func missingObmondoSupportConfig(cfg *PromptedConfig) bool {
	if !obmondoSupportEnabled(cfg) {
		return false
	}
	return cfg.Obmondo.CertPath == "" || cfg.Obmondo.KeyPath == ""
}
