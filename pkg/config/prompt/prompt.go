// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package prompt

import (
	_ "embed"
	"fmt"
	"path"

	"github.com/charmbracelet/huh"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
)

var (
	//go:embed templates/general.yaml.tmpl
	generalConfigTemplate string

	//go:embed templates/secrets.yaml.tmpl
	secretsConfigTemplate string
)

// PromptedConfig holds all the values collected from interactive prompts and auto-detection.
type PromptedConfig struct {
	// Cluster.
	ClusterName           string
	ClusterType           string
	K8sVersion            string
	KubePrometheusVersion string
	EnableAuditLogging    bool

	// Git.
	UseSSHAgent bool
	SSHKeyPath  string
	SSHUsername string

	// Forks.
	KubeaidForkURL       string
	KubeaidVersion       string
	KubeaidConfigForkURL string
	KubeaidConfigDir     string

	// ArgoCD deploy keys.
	KubeaidConfigDeployKeyPath string

	// Cloud provider.
	CloudProvider string

	// AWS.
	AWSRegion          string
	AWSSSHKeyName      string
	AWSCPInstanceType  string
	AWSCPReplicas      string
	AWSAMIID           string
	AWSAccessKeyID     string
	AWSSecretAccessKey string
	AWSSessionToken    string

	// Azure.
	AzureTenantID       string
	AzureSubscriptionID string
	AzureLocation       string
	AzureStorageAccount string
	AzureCPVMSize       string
	AzureCPReplicas     string
	AzureCPDiskSizeGB   string
	AzureClientID       string
	AzureClientSecret   string

	// Hetzner.
	HetznerMode          string
	HetznerSSHKeyName    string
	HetznerSSHKeyPath    string
	HetznerHCloudZone    string
	HetznerCPMachineType string
	HetznerCPReplicas    string
	HetznerLBRegion      string
	HetznerRegion        string
	HetznerAPIToken      string
	HetznerRobotUser     string
	HetznerRobotPassword string

	// Bare Metal.
	BareMetalSSHPort      string
	BareMetalEndpointHost string
	BareMetalEndpointPort string
}

// ConfigFromPrompt interactively collects required configuration parameters from the user
// and writes the generated config files to configsDirectory.
//
// The flow is:
//   - Phase 1: Auto-detect K8s version (latest-1), KubeAid version (latest-1), SSH agent
//   - Phase 2: Ask for cloud provider first, then delegate to the provider-specific prompter
//   - Phase 3: Print summary and confirm
func ConfigFromPrompt(configsDirectory string) error {
	// Phase 1: Auto-detect.
	detected := autoDetect()

	cfg := &PromptedConfig{
		// SRE defaults.
		ClusterType:           "workload",
		SSHUsername:           "git",
		KubeaidForkURL:        constants.KubeAidPublicHTTPSURL,
		K8sVersion:            detected.K8sVersion,
		KubePrometheusVersion: detected.KubePrometheusVersion,
		KubeaidVersion:        detected.KubeAidVersion,
	}

	// Phase 2: Cluster configuration — provider first, then provider-specific questions.
	printSectionHeader("Cluster Configuration")

	if err := promptProvider(cfg); err != nil {
		return fmt.Errorf("collecting cloud provider: %w", err)
	}

	if err := promptClusterName(cfg); err != nil {
		return fmt.Errorf("collecting cluster name: %w", err)
	}

	// Delegate remaining questions to the provider-specific prompter.
	prompter := prompterForProvider(cfg.CloudProvider)
	if err := prompter.PromptConfig(cfg, detected); err != nil {
		return fmt.Errorf("collecting provider config: %w", err)
	}

	// Phase 3: Summary and confirm.
	if err := printSummaryAndConfirm(cfg); err != nil {
		return fmt.Errorf("confirming config: %w", err)
	}

	// Expand tilde in all file paths so that paths are absolute.
	expandPaths(cfg)

	if err := writeConfigFiles(configsDirectory, cfg); err != nil {
		return fmt.Errorf("writing config files: %w", err)
	}

	return nil
}

func promptClusterName(cfg *PromptedConfig) error {
	return requiredInput("Cluster name:", &cfg.ClusterName)
}

// promptHAControlPlane asks whether the user wants a highly available control plane
// and returns the appropriate replica count ("3" for HA, "1" otherwise).
func promptHAControlPlane() (string, error) {
	var ha bool
	if err := confirm("Enable high availability for the control plane?", true, &ha); err != nil {
		return "", err
	}

	if ha {
		return "3", nil
	}

	return "1", nil
}

func promptConfigRepo(cfg *PromptedConfig) error {
	const message = "KubeAid Config fork SSH URL:"
	cfg.KubeaidConfigForkURL = "git@github.com:Obmondo/kubeaid-config.git"
	if err := huh.NewInput().
		Title(message).
		Value(&cfg.KubeaidConfigForkURL).
		Validate(nonEmpty).
		Run(); err != nil {
		return err
	}
	printRecap(message, cfg.KubeaidConfigForkURL)
	return nil
}

func promptDeployKeyPath(cfg *PromptedConfig) error {
	return promptSSHPrivateKeyPath(
		&cfg.KubeaidConfigDeployKeyPath,
		"ArgoCD deploy key (private key file path):",
	)
}

func promptGitSSHKey(cfg *PromptedConfig) error {
	return promptSSHPrivateKeyPath(
		&cfg.SSHKeyPath,
		"Git SSH private key path:",
	)
}

func promptProvider(cfg *PromptedConfig) error {
	return selectOption(
		"Cloud provider:",
		[]string{
			constants.CloudProviderAWS,
			constants.CloudProviderAzure,
			constants.CloudProviderHetzner,
			constants.CloudProviderBareMetal,
			constants.CloudProviderLocal,
		},
		"",
		&cfg.CloudProvider,
	)
}

// promptSSHAuth resolves SSH authentication and config repo URL.
// The flow depends on whether the SSH agent (YubiKey) is available:
//
// With YubiKey: ArgoCD deploy key -> config repo URL
// Without YubiKey: ArgoCD deploy key -> config repo URL -> Git SSH key (verified)
func promptSSHAuth(cfg *PromptedConfig, detected *autoDetectedConfig) error {
	if detected.SSHAgentAvail {
		cfg.UseSSHAgent = true

		if err := promptDeployKeyPath(cfg); err != nil {
			return err
		}

		return promptConfigRepo(cfg)
	}

	// No SSH agent (no YubiKey): ask for separate ArgoCD and Git keys.
	if err := promptDeployKeyPath(cfg); err != nil {
		return err
	}

	if err := promptConfigRepo(cfg); err != nil {
		return err
	}

	return promptGitSSHKey(cfg)
}

// writeConfigFiles renders the config templates with prompted values and writes them to disk.
func writeConfigFiles(configsDirectory string, cfg *PromptedConfig) error {
	generalPath := path.Join(configsDirectory, "general.yaml")
	if err := writeTemplatedFile(generalPath, generalConfigTemplate, cfg, 0o600); err != nil {
		return fmt.Errorf("writing general config: %w", err)
	}

	secretsPath := path.Join(configsDirectory, "secrets.yaml")
	if err := writeTemplatedFile(secretsPath, secretsConfigTemplate, cfg, 0o600); err != nil {
		return fmt.Errorf("writing secrets config: %w", err)
	}

	return nil
}
