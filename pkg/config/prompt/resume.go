// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package prompt

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"strconv"

	"github.com/charmbracelet/huh"
	"gopkg.in/yaml.v3"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
)

const promptStateFileName = ".kubeaid-prompt-state.yaml"

func promptStatePath(configsDirectory string) string {
	return path.Join(configsDirectory, promptStateFileName)
}

func loadPromptState(configsDirectory string) (promptState, bool, error) {
	data, err := os.ReadFile(promptStatePath(configsDirectory))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return promptState{}, false, nil
		}
		return promptState{}, false, fmt.Errorf("reading %s: %w", promptStateFileName, err)
	}

	var state promptState
	if err := yaml.Unmarshal(data, &state); err != nil {
		return promptState{}, false, fmt.Errorf("parsing %s: %w", promptStateFileName, err)
	}

	return state, true, nil
}

func writePromptState(configsDirectory string, state *promptState) error {
	if state == nil {
		state = &promptState{}
	}

	if err := os.MkdirAll(configsDirectory, 0o750); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	data, err := yaml.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshalling prompt state: %w", err)
	}

	if err := os.WriteFile(promptStatePath(configsDirectory), data, 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", promptStateFileName, err)
	}

	return nil
}

func removePromptState(configsDirectory string) error {
	if err := os.Remove(promptStatePath(configsDirectory)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

func existingPromptConfigPresent(configsDirectory string) bool {
	for _, filename := range []string{"general.yaml", "secrets.yaml", promptStateFileName} {
		if _, err := os.Stat(path.Join(configsDirectory, filename)); err == nil {
			return true
		}
	}
	return false
}

func confirmLoadExistingConfig(configsDirectory string) (bool, error) {
	loadExisting := true
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Existing config found. Load it and continue the prompt?").
				Description(configsDirectory + "\nFiles are rewritten only after the final confirm.").
				Affirmative("Load existing").
				Negative("Start fresh").
				Value(&loadExisting),
		),
	).Run()
	if err != nil {
		return false, err
	}
	return loadExisting, nil
}

func loadExistingPromptedConfig(configsDirectory string, cfg *PromptedConfig) error {
	if cfg == nil {
		return errors.New("prompted config is nil")
	}

	generalPath := path.Join(configsDirectory, "general.yaml")
	secretsPath := path.Join(configsDirectory, "secrets.yaml")

	generalLoaded := false
	if data, err := os.ReadFile(generalPath); err == nil {
		generalLoaded = true
		var general config.GeneralConfig
		//nolint:musttag // GeneralConfig is the YAML schema type; hydrated runtime fields are intentionally untagged.
		if err := yaml.Unmarshal(data, &general); err != nil {
			return fmt.Errorf("parsing %s: %w", generalPath, err)
		}
		applyGeneralConfigToPromptedConfig(&general, cfg)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("reading %s: %w", generalPath, err)
	}

	secretsLoaded := false
	if data, err := os.ReadFile(secretsPath); err == nil {
		secretsLoaded = true
		var secrets config.SecretsConfig
		if err := yaml.Unmarshal(data, &secrets); err != nil {
			return fmt.Errorf("parsing %s: %w", secretsPath, err)
		}
		applySecretsConfigToPromptedConfig(&secrets, cfg)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("reading %s: %w", secretsPath, err)
	}

	if !generalLoaded && !secretsLoaded {
		return fs.ErrNotExist
	}

	return nil
}

func loadExistingPromptedConfigIfPresent(configsDirectory string, cfg *PromptedConfig) (bool, error) {
	if err := loadExistingPromptedConfig(configsDirectory, cfg); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func applyGeneralConfigToPromptedConfig(general *config.GeneralConfig, cfg *PromptedConfig) {
	if general == nil || cfg == nil {
		return
	}

	cfg.SSHUsername = firstNonEmpty(general.Git.SSHUsername, cfg.SSHUsername)
	if general.Git.SSHKeyPairConfig != nil {
		cfg.UseSSHAgent = general.Git.UseSSHAgent
		cfg.SSHKeyPath = firstNonEmpty(general.Git.PrivateKeyFilePath, cfg.SSHKeyPath)
	}
	if len(general.Git.KnownHosts) > 0 {
		cfg.GitKnownHosts = append([]string(nil), general.Git.KnownHosts...)
	}

	cfg.KubeaidForkURL = firstNonEmpty(general.Forks.KubeaidFork.URL, cfg.KubeaidForkURL)
	cfg.KubeaidVersion = firstNonEmpty(general.Forks.KubeaidFork.Version, cfg.KubeaidVersion)
	cfg.KubeaidConfigForkURL = firstNonEmpty(general.Forks.KubeaidConfigFork.URL, cfg.KubeaidConfigForkURL)
	cfg.KubeaidConfigDir = firstNonEmpty(general.Forks.KubeaidConfigFork.Directory, cfg.KubeaidConfigDir)

	cfg.ClusterName = firstNonEmpty(general.Cluster.Name, cfg.ClusterName)
	cfg.ClusterType = firstNonEmpty(general.Cluster.Type, cfg.ClusterType)
	cfg.K8sVersion = firstNonEmpty(general.Cluster.K8sVersion, cfg.K8sVersion)
	cfg.EnableAuditLogging = general.Cluster.EnableAuditLogging
	cfg.ACMEEmail = firstNonEmpty(general.Cluster.ACMEEmail, cfg.ACMEEmail)

	if general.Cluster.APIServer.OIDC != nil {
		cfg.EnableOIDC = true
		cfg.OIDCIssuerURL = firstNonEmpty(general.Cluster.APIServer.OIDC.IssuerURL, cfg.OIDCIssuerURL)
		cfg.OIDCClientID = firstNonEmpty(general.Cluster.APIServer.OIDC.ClientID, cfg.OIDCClientID)
	}

	if general.Cluster.Keycloak != nil {
		cfg.KeycloakMode = firstNonEmpty(general.Cluster.Keycloak.Mode, cfg.KeycloakMode)
		cfg.KeycloakDNS = firstNonEmpty(general.Cluster.Keycloak.DNS, cfg.KeycloakDNS)
		cfg.KeycloakRealm = firstNonEmpty(general.Cluster.Keycloak.Realm, cfg.KeycloakRealm)
		if cfg.ClusterType == constants.ClusterTypeWorkload {
			cfg.EnableOIDC = true
		}
	}

	if general.Cluster.NetBird != nil {
		cfg.NetBirdDNS = firstNonEmpty(general.Cluster.NetBird.DNS, cfg.NetBirdDNS)
		cfg.NetBirdDNSZone = firstNonEmpty(general.Cluster.NetBird.DNSZone, cfg.NetBirdDNSZone)
	}

	cfg.KubeaidConfigDeployKeyPath = firstNonEmpty(
		general.Cluster.ArgoCD.DeployKeys.KubeaidConfig.PrivateKeyFilePath,
		cfg.KubeaidConfigDeployKeyPath,
	)
	if cfg.KubeaidConfigDeployKeyPath == "" && general.Cluster.ArgoCD.DeployKeys.Kubeaid != nil {
		cfg.KubeaidConfigDeployKeyPath = general.Cluster.ArgoCD.DeployKeys.Kubeaid.PrivateKeyFilePath
	}

	if general.KubePrometheus != nil {
		cfg.KubePrometheusVersion = firstNonEmpty(general.KubePrometheus.Version, cfg.KubePrometheusVersion)
	}
	if general.Obmondo != nil {
		obmondo := ensureObmondoConfig(cfg)
		obmondo.CustomerID = firstNonEmpty(general.Obmondo.CustomerID, obmondo.CustomerID)
		obmondo.Monitoring = general.Obmondo.Monitoring
		obmondo.CertPath = firstNonEmpty(general.Obmondo.CertPath, obmondo.CertPath)
		obmondo.KeyPath = firstNonEmpty(general.Obmondo.KeyPath, obmondo.KeyPath)
	}

	applyCloudConfigToPromptedConfig(&general.Cloud, cfg)
}

func applyCloudConfigToPromptedConfig(cloud *config.CloudConfig, cfg *PromptedConfig) {
	if cloud == nil || cfg == nil {
		return
	}

	switch {
	case cloud.AWS != nil:
		cfg.CloudProvider = constants.CloudProviderAWS
		cfg.AWSRegion = firstNonEmpty(cloud.AWS.Region, cfg.AWSRegion)
		cfg.AWSSSHKeyName = firstNonEmpty(cloud.AWS.SSHKeyName, cfg.AWSSSHKeyName)
		cfg.AWSCPInstanceType = firstNonEmpty(cloud.AWS.ControlPlane.InstanceType, cfg.AWSCPInstanceType)
		cfg.AWSCPReplicas = firstNonEmpty(uint32String(cloud.AWS.ControlPlane.Replicas), cfg.AWSCPReplicas)
		cfg.AWSAMIID = firstNonEmpty(cloud.AWS.ControlPlane.AMI.ID, cfg.AWSAMIID)
	case cloud.Azure != nil:
		cfg.CloudProvider = constants.CloudProviderAzure
		cfg.AzureTenantID = firstNonEmpty(cloud.Azure.TenantID, cfg.AzureTenantID)
		cfg.AzureSubscriptionID = firstNonEmpty(cloud.Azure.SubscriptionID, cfg.AzureSubscriptionID)
		cfg.AzureLocation = firstNonEmpty(cloud.Azure.Location, cfg.AzureLocation)
		cfg.AzureStorageAccount = firstNonEmpty(cloud.Azure.StorageAccount, cfg.AzureStorageAccount)
		cfg.AzureCPVMSize = firstNonEmpty(cloud.Azure.ControlPlane.VMSize, cfg.AzureCPVMSize)
		cfg.AzureCPReplicas = firstNonEmpty(uint32String(cloud.Azure.ControlPlane.Replicas), cfg.AzureCPReplicas)
		cfg.AzureCPDiskSizeGB = firstNonEmpty(uint32String(cloud.Azure.ControlPlane.DiskSizeGB), cfg.AzureCPDiskSizeGB)
	case cloud.Hetzner != nil:
		applyHetznerConfigToPromptedConfig(cloud.Hetzner, cfg)
	case cloud.BareMetal != nil:
		cfg.CloudProvider = constants.CloudProviderBareMetal
		cfg.BareMetalSSHPort = firstNonEmpty(uintString(cloud.BareMetal.SSH.Port), cfg.BareMetalSSHPort)
		cfg.BareMetalEndpointHost = firstNonEmpty(
			cloud.BareMetal.ControlPlane.Endpoint.Host,
			cfg.BareMetalEndpointHost,
		)
		cfg.BareMetalEndpointPort = firstNonEmpty(
			uintString(cloud.BareMetal.ControlPlane.Endpoint.Port),
			cfg.BareMetalEndpointPort,
		)
	case cloud.Local != nil:
		cfg.CloudProvider = constants.CloudProviderLocal
	}
}

func applyHetznerConfigToPromptedConfig(hetzner *config.HetznerConfig, cfg *PromptedConfig) {
	if hetzner == nil || cfg == nil {
		return
	}

	cfg.CloudProvider = constants.CloudProviderHetzner
	cfg.HetznerMode = firstNonEmpty(hetzner.Mode, cfg.HetznerMode)
	cfg.HetznerSSHKeyName = firstNonEmpty(hetzner.SSHKeyPair.Name, cfg.HetznerSSHKeyName)
	cfg.HetznerSSHKeyPath = firstNonEmpty(hetzner.SSHKeyPair.PrivateKeyFilePath, cfg.HetznerSSHKeyPath)

	if hetzner.HCloud != nil {
		cfg.HetznerHCloudZone = firstNonEmpty(hetzner.HCloud.Zone, cfg.HetznerHCloudZone)
	}
	if hetzner.ControlPlane.HCloud != nil {
		hcloud := hetzner.ControlPlane.HCloud
		cfg.HetznerCPMachineType = firstNonEmpty(hcloud.MachineType, cfg.HetznerCPMachineType)
		cfg.HetznerCPReplicas = firstNonEmpty(uintString(hcloud.Replicas), cfg.HetznerCPReplicas)
		cfg.HetznerLBRegion = firstNonEmpty(hcloud.LoadBalancer.Region, cfg.HetznerLBRegion)
		cfg.ControlPlaneEndpoint = firstNonEmpty(hcloud.LoadBalancer.Endpoint, cfg.ControlPlaneEndpoint)
	}
	if len(hetzner.ControlPlane.Regions) > 0 {
		cfg.HetznerRegion = firstNonEmpty(hetzner.ControlPlane.Regions[0], cfg.HetznerRegion)
		// Mirror the whole list onto cfg.HetznerBMCPRegions so a
		// resumed bare-metal session re-renders the same regions
		// rather than dropping back to the empty list the template
		// used to emit. hcloud / hybrid modes only use the first
		// element via HetznerRegion above, so the extra copy here
		// is harmless for them.
		cfg.HetznerBMCPRegions = append([]string{}, hetzner.ControlPlane.Regions...)
	}
	if hetzner.BareMetal != nil && hetzner.BareMetal.VSwitch != nil {
		vSwitch := hetzner.BareMetal.VSwitch
		cfg.HetznerVSwitchName = firstNonEmpty(vSwitch.Name, cfg.HetznerVSwitchName)
		cfg.HetznerVSwitchVLANID = firstNonEmpty(intString(vSwitch.VLANID), cfg.HetznerVSwitchVLANID)
		cfg.HetznerVSwitchSubnetCIDR = firstNonEmpty(
			vSwitch.SubnetCIDRBlock,
			cfg.HetznerVSwitchSubnetCIDR,
		)
	}
	if hetzner.ControlPlane.BareMetal != nil {
		bareMetal := hetzner.ControlPlane.BareMetal
		cfg.HetznerBMEndpointHost = firstNonEmpty(
			bareMetal.Endpoint.Host,
			cfg.HetznerBMEndpointHost,
		)
		cfg.HetznerBMEndpointIsFailoverIP = bareMetal.Endpoint.IsFailoverIP
		if len(bareMetal.BareMetalHosts) > 0 {
			cfg.HetznerCPReplicas = firstNonEmpty(
				strconv.Itoa(len(bareMetal.BareMetalHosts)),
				cfg.HetznerCPReplicas,
			)
			cfg.HetznerBMCPServerIDs, cfg.HetznerBMCPPrivateIPs = hetznerBareMetalHostValues(
				bareMetal.BareMetalHosts,
			)
		}
	}
	if len(hetzner.NodeGroups.BareMetal) > 0 {
		nodeGroup := firstHetznerBareMetalNodeGroup(hetzner.NodeGroups.BareMetal)
		if nodeGroup != nil {
			cfg.HetznerBMNodeGroupName = firstNonEmpty(nodeGroup.Name, cfg.HetznerBMNodeGroupName)
			cfg.HetznerBMNodeGroupServerIDs, cfg.HetznerBMNodeGroupPrivateIPs = hetznerBareMetalHostValues(
				nodeGroup.BareMetalHosts,
			)
		}
	}
}

func applySecretsConfigToPromptedConfig(secrets *config.SecretsConfig, cfg *PromptedConfig) {
	if secrets == nil || cfg == nil {
		return
	}

	if secrets.AWS != nil {
		cfg.AWSAccessKeyID = firstNonEmpty(secrets.AWS.AWSAccessKeyID, cfg.AWSAccessKeyID)
		cfg.AWSSecretAccessKey = firstNonEmpty(secrets.AWS.AWSSecretAccessKey, cfg.AWSSecretAccessKey)
		cfg.AWSSessionToken = firstNonEmpty(secrets.AWS.AWSSessionToken, cfg.AWSSessionToken)
	}
	if secrets.Azure != nil {
		cfg.AzureClientID = firstNonEmpty(secrets.Azure.ClientID, cfg.AzureClientID)
		cfg.AzureClientSecret = firstNonEmpty(secrets.Azure.ClientSecret, cfg.AzureClientSecret)
	}
	if secrets.Hetzner != nil {
		cfg.HetznerAPIToken = firstNonEmpty(secrets.Hetzner.APIToken, cfg.HetznerAPIToken)
		if secrets.Hetzner.Robot != nil {
			cfg.HetznerRobotUser = firstNonEmpty(secrets.Hetzner.Robot.User, cfg.HetznerRobotUser)
			cfg.HetznerRobotPassword = firstNonEmpty(
				secrets.Hetzner.Robot.Password,
				cfg.HetznerRobotPassword,
			)
		}
	}
	if secrets.NetBird != nil {
		cfg.NetBirdAPIKey = firstNonEmpty(secrets.NetBird.APIKey, cfg.NetBirdAPIKey)
	}
	if secrets.Keycloak != nil {
		cfg.NetBirdBackendClientSecret = firstNonEmpty(
			secrets.Keycloak.NetBirdBackendClientSecret,
			cfg.NetBirdBackendClientSecret,
		)
	}
}

func completedPromptStateFromValues(cfg *PromptedConfig) promptState {
	return promptState{
		K8sProfile:          cfg.K8sVersion != "",
		Basics:              !missingBasics(cfg),
		VPNKeycloak:         cfg.ClusterType != constants.ClusterTypeVPN || !missingVPNKeycloak(cfg),
		VPNEndpoints:        cfg.ClusterType != constants.ClusterTypeVPN || !missingVPNEndpoints(cfg),
		WorkloadKeycloak:    cfg.ClusterType == constants.ClusterTypeVPN || !missingWorkloadKeycloak(cfg),
		ProviderCredentials: !missingProviderPromptConfig(cfg),
		GitSSH:              !missingGitSSH(cfg),
		ObmondoSupport:      !missingObmondoSupportConfig(cfg),
		NetBirdDNSZone:      cfg.NetBirdDNSZone != "",
	}
}

func missingClusterModeConfig(cfg *PromptedConfig) bool {
	if cfg.ClusterType == constants.ClusterTypeVPN {
		return missingVPNKeycloak(cfg) || missingVPNEndpoints(cfg)
	}
	return missingWorkloadKeycloak(cfg)
}

func missingProviderRenderedConfig(cfg *PromptedConfig) bool {
	switch cfg.CloudProvider {
	case constants.CloudProviderAWS:
		return missingAWSRenderedConfig(cfg)
	default:
		return missingProviderPromptConfig(cfg)
	}
}

func missingAWSRenderedConfig(cfg *PromptedConfig) bool {
	return missingAWSPromptConfig(cfg) || cfg.AWSSSHKeyName == ""
}

func firstNonEmpty(candidate string, fallback string) string {
	if candidate != "" {
		return candidate
	}
	return fallback
}

func firstHetznerBareMetalNodeGroup(
	nodeGroups []*config.HetznerBareMetalNodeGroup,
) *config.HetznerBareMetalNodeGroup {
	for _, nodeGroup := range nodeGroups {
		if nodeGroup != nil {
			return nodeGroup
		}
	}
	return nil
}

func hetznerBareMetalHostValues(hosts []*config.HetznerBareMetalHost) ([]string, []string) {
	serverIDs := make([]string, 0, len(hosts))
	privateIPs := make([]string, 0, len(hosts))
	for _, host := range hosts {
		if host == nil {
			continue
		}
		serverIDs = append(serverIDs, host.ServerID)
		privateIPs = append(privateIPs, host.PrivateIP)
	}
	return serverIDs, privateIPs
}

func intString(value int) string {
	if value == 0 {
		return ""
	}
	return strconv.Itoa(value)
}

func uint32String(value uint32) string {
	if value == 0 {
		return ""
	}
	return strconv.FormatUint(uint64(value), 10)
}

func uintString(value uint) string {
	if value == 0 {
		return ""
	}
	return strconv.FormatUint(uint64(value), 10)
}
