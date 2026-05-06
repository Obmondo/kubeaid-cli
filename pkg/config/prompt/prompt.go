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

	// OIDC for kube-apiserver. EnableOIDC gates whether the apiServer
	// .oidc block is rendered into general.yaml at all; when false,
	// the template emits a commented-out placeholder so the user can
	// fill it in by hand later. UsernameClaim/GroupsClaim default to
	// "email"/"groups" in the schema, so we don't prompt for them.
	EnableOIDC    bool
	OIDCIssuerURL string
	OIDCClientID  string

	// VPN-cluster fields. Populated only when the operator chose
	// the "VPN cluster" kind at the cluster-kind prompt — i.e.
	// cluster.type=vpn. Render the cluster.keycloak /
	// cluster.netbird / cluster.acmeEmail blocks in general.yaml.
	// Empty for workload clusters.
	KeycloakMode string // "managed" | "external"
	KeycloakDNS  string
	NetBirdDNS   string
	ACMEEmail    string

	// NetBirdBackendClientSecret is collected only when KeycloakMode
	// is "external" — kubeaid-cli has no way to mint or look up the
	// netbird-backend client secret in the operator's external
	// Keycloak. Rendered into secrets.yaml under
	// keycloak.netBirdBackendClientSecret. Empty when managed.
	NetBirdBackendClientSecret string

	// HCloud-VPN control-plane endpoint FQDN — required when
	// running a VPN cluster on Hetzner HCloud. Rendered into
	// cloud.hetzner.controlPlane.hcloud.loadBalancer.endpoint;
	// must resolve (post-DNS-setup) to the LB's public IP during
	// bootstrap and to its private IP afterwards.
	ControlPlaneEndpoint string

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

	if err := promptClusterKind(cfg); err != nil {
		return fmt.Errorf("collecting cluster kind: %w", err)
	}

	if cfg.ClusterType == constants.ClusterTypeVPN {
		if err := promptVPNClusterDetails(cfg); err != nil {
			return fmt.Errorf("collecting VPN cluster details: %w", err)
		}
	} else if err := promptOIDC(cfg); err != nil {
		return fmt.Errorf("collecting OIDC config: %w", err)
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

// promptOIDC asks whether to enable OIDC on kube-apiserver and, if so,
// collects the issuer URL and client ID — the only two required
// fields. UsernameClaim and GroupsClaim are defaulted by the schema.
// Default is "no" so users who don't run Keycloak get a clean
// non-OIDC config without extra prompts.
func promptOIDC(cfg *PromptedConfig) error {
	if err := confirm(
		"Enable OIDC (Keycloak) authentication on kube-apiserver?",
		false, &cfg.EnableOIDC,
	); err != nil {
		return err
	}

	if !cfg.EnableOIDC {
		return nil
	}

	if err := requiredHTTPSInput(
		"OIDC issuer URL (e.g. https://keycloak.example/realms/clusters):",
		&cfg.OIDCIssuerURL,
	); err != nil {
		return err
	}

	return requiredInput(
		fmt.Sprintf("OIDC client ID for this cluster (e.g. kubernetes-%s):", cfg.ClusterName),
		&cfg.OIDCClientID,
	)
}

// promptClusterKind asks the operator whether they're setting up a
// brand-new VPN cluster (Phase 0 — hosts Keycloak + NetBird mesh) or
// a regular workload cluster. Maps the choice to cfg.ClusterType.
//
// VPN clusters are only supported on Hetzner HCloud today; for any
// other provider the prompt is skipped and the cluster is treated as
// workload. (The schema validator rejects vpn-on-non-hcloud at parse
// time, so the prompt mirrors that constraint up front.)
func promptClusterKind(cfg *PromptedConfig) error {
	if cfg.CloudProvider != constants.CloudProviderHetzner {
		cfg.ClusterType = constants.ClusterTypeWorkload
		return nil
	}

	const (
		optVPN      = "A new VPN cluster (Phase 0 — hosts Keycloak + NetBird mesh)"
		optWorkload = "A workload cluster (no managed Keycloak; OIDC is optional)"
	)

	var choice string
	if err := selectOption(
		"What are you setting up?",
		[]string{optVPN, optWorkload},
		optWorkload, &choice,
	); err != nil {
		return err
	}

	if choice == optVPN {
		cfg.ClusterType = constants.ClusterTypeVPN
	} else {
		cfg.ClusterType = constants.ClusterTypeWorkload
	}

	return nil
}

// promptVPNClusterDetails collects the inputs every VPN cluster
// needs: Keycloak mode (managed/external), Keycloak DNS, NetBird DNS,
// ACME email for Let's Encrypt, and the control-plane endpoint FQDN.
//
// In external mode it also collects the netbird-backend OIDC client
// secret (kubeaid-cli can't mint it because the client lives in the
// operator's external Keycloak) and prints a pointer to the realm
// prerequisites doc so the operator knows what to set up there.
//
// Auto-derives apiServer.oidc.{issuerUrl,clientId} from Keycloak DNS
// and cluster name; the realm name is re-derived at parse time via
// publicsuffix.
func promptVPNClusterDetails(cfg *PromptedConfig) error {
	if err := promptKeycloakMode(cfg); err != nil {
		return err
	}

	if err := requiredInput(
		"Keycloak DNS (e.g. keycloak.vpn.acme.com):",
		&cfg.KeycloakDNS,
	); err != nil {
		return err
	}

	// Strip the leading "keycloak." (or whatever first label is)
	// from the Keycloak DNS to build defaults for the next three
	// prompts: a base of "vpn.obmondo.com" yields "netbird.vpn.obmondo.com",
	// "api.vpn.obmondo.com", and "ops@obmondo.com" without the
	// operator retyping the suffix. Empty base (single-label host
	// like "localhost") falls through to a blank prompt.
	if base := stripFirstLabel(cfg.KeycloakDNS); base != "" {
		if cfg.NetBirdDNS == "" {
			cfg.NetBirdDNS = "netbird." + base
		}
		if cfg.ControlPlaneEndpoint == "" {
			cfg.ControlPlaneEndpoint = "api." + base
		}
		if cfg.ACMEEmail == "" {
			cfg.ACMEEmail = deriveACMEEmailFromDNS(base)
		}
	}

	if err := requiredInput(
		"NetBird Mgmt DNS (e.g. netbird.vpn.acme.com):",
		&cfg.NetBirdDNS,
	); err != nil {
		return err
	}

	if err := requiredInput(
		"Control-plane endpoint FQDN (e.g. api.vpn.acme.com):",
		&cfg.ControlPlaneEndpoint,
	); err != nil {
		return err
	}

	if err := requiredInput(
		"ACME email for Let's Encrypt (e.g. ops@acme.com):",
		&cfg.ACMEEmail,
	); err != nil {
		return err
	}

	if cfg.KeycloakMode == constants.KeycloakModeExternal {
		if err := requiredPassword(
			"netbird-backend client secret (from your external Keycloak):",
			&cfg.NetBirdBackendClientSecret,
		); err != nil {
			return err
		}
	}

	cfg.EnableOIDC = true
	cfg.OIDCIssuerURL = "https://" + cfg.KeycloakDNS + "/realms/" + deriveRealmFromDNS(cfg.KeycloakDNS)
	cfg.OIDCClientID = "kubernetes-" + cfg.ClusterName

	return nil
}

// promptKeycloakMode asks whether kubeaid-cli should install Keycloak
// itself (managed) or hook into the operator's existing Keycloak
// (external). External mode prints a pointer to the doc that
// enumerates the realm prerequisites the operator must set up by
// hand — netbird-client / netbird-backend / kubernetes-<cluster>
// clients, the api scope, the audience mapper, the view-users grant.
func promptKeycloakMode(cfg *PromptedConfig) error {
	const (
		optManaged  = "managed (kubeaid-cli installs Keycloak on this cluster)"
		optExternal = "external (use my existing Keycloak elsewhere)"
	)

	var choice string
	if err := selectOption(
		"Keycloak mode:",
		[]string{optManaged, optExternal},
		optManaged, &choice,
	); err != nil {
		return err
	}

	if choice == optManaged {
		cfg.KeycloakMode = constants.KeycloakModeManaged
		return nil
	}

	cfg.KeycloakMode = constants.KeycloakModeExternal
	fmt.Println()
	fmt.Println("  External Keycloak selected. Before running bootstrap, make sure your")
	fmt.Println("  Keycloak realm has the resources kubeaid-cli would otherwise create:")
	fmt.Println("  three OIDC clients (netbird-client, netbird-backend, kubernetes-<cluster>),")
	fmt.Println("  one client scope ('api'), one audience mapper, and the view-users role")
	fmt.Println("  grant on netbird-backend. See:")
	fmt.Println()
	fmt.Println("    kubeaid/argocd-helm-charts/netbird/README.md")
	fmt.Println("    -> 'Keycloak realm prerequisites'")
	fmt.Println()

	return nil
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
