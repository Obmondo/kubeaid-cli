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

// ConfigFromPrompt interactively collects required configuration parameters from
// the user and writes the generated config files to configsDirectory.
//
// The flow is:
//   - Phase 1: Auto-detect K8s version (latest-1), KubeAid version (latest-1), SSH agent.
//   - Phase 2: Grouped form prompts. Related fields are shown together on the
//     same screen. Steps:
//     Step 1 — Cluster basics (provider, name, kind)
//     Step 2a — VPN Keycloak setup (mode + DNS) — hidden for workload clusters
//     Step 2b — VPN endpoints (NetBird DNS, CP endpoint, ACME email) — hidden
//     for workload clusters; pre-filled by auto-derive from Keycloak DNS
//     Step 2c — OIDC (optional) — hidden for VPN clusters
//     Step 3 — Cloud credentials (provider-specific)
//     Step 4 — Git/SSH (deploy key, config repo, optional Git SSH key)
//   - Phase 3: Print summary; "Looks good?" confirm. Loop back to Phase 2 on No.
func ConfigFromPrompt(configsDirectory string) error {
	// Phase 1: Auto-detect.
	detected := autoDetect()

	cfg := &PromptedConfig{
		// SRE defaults.
		ClusterType:           constants.ClusterTypeWorkload,
		SSHUsername:           "git",
		KubeaidForkURL:        constants.KubeAidPublicHTTPSURL,
		KubeaidConfigForkURL:  "git@github.com:Obmondo/kubeaid-config.git",
		K8sVersion:            detected.K8sVersion,
		KubePrometheusVersion: detected.KubePrometheusVersion,
		KubeaidVersion:        detected.KubeAidVersion,
		// Hetzner defaults — pre-set so the form pre-fills them on
		// edit loops even before the Hetzner group has run once.
		HetznerMode:          "hcloud",
		HetznerHCloudZone:    "eu-central",
		HetznerCPMachineType: "cax21",
		HetznerRegion:        "hel1",
	}

	// Phase 2 + 3: Prompt loop — re-runs when the operator declines the summary.
	for {
		// Step 1: cluster basics — provider, name, kind.
		if err := runBasicsForm(cfg); err != nil {
			return fmt.Errorf("collecting cluster basics: %w", err)
		}

		// Steps 2a/2b/2c: VPN or OIDC details.
		if cfg.ClusterType == constants.ClusterTypeVPN {
			if err := runVPNKeycloakForm(cfg); err != nil {
				return fmt.Errorf("collecting VPN Keycloak setup: %w", err)
			}
			// Auto-derive VPN DNS defaults from Keycloak DNS after group A.
			applyVPNDefaults(cfg)

			if err := runVPNEndpointsForm(cfg); err != nil {
				return fmt.Errorf("collecting VPN endpoints: %w", err)
			}

			// Derive OIDC from Keycloak DNS — no separate prompt needed for VPN clusters.
			cfg.EnableOIDC = true
			cfg.OIDCIssuerURL = "https://" + cfg.KeycloakDNS + "/realms/" + deriveRealmFromDNS(cfg.KeycloakDNS)
			cfg.OIDCClientID = "kubernetes-" + cfg.ClusterName
		} else {
			if err := runOIDCForm(cfg); err != nil {
				return fmt.Errorf("collecting OIDC config: %w", err)
			}
		}

		// Step 3: provider-specific credentials.
		prompter := prompterForProvider(cfg.CloudProvider)
		if err := prompter.RunCredentialsForm(cfg, detected); err != nil {
			return fmt.Errorf("collecting provider credentials: %w", err)
		}

		// Step 4: Git / SSH.
		if err := runGitSSHForm(cfg, detected); err != nil {
			return fmt.Errorf("collecting Git/SSH config: %w", err)
		}

		// AWS derives its SSH key pair name from the deploy key path,
		// which is only known after Step 4.
		if aws, ok := prompterForProvider(cfg.CloudProvider).(*awsPrompter); ok {
			aws.postProcess(cfg)
		}

		// Phase 3: summary + confirm.
		printSummary(cfg)

		confirmed, err := runConfirm()
		if err != nil {
			return fmt.Errorf("confirming config: %w", err)
		}
		if confirmed {
			break
		}
		// Operator picked No — loop back; all cfg fields carry the
		// last-entered values so the form reopens pre-filled.
	}

	// Expand tilde in all file paths so that paths are absolute.
	expandPaths(cfg)

	if err := writeConfigFiles(configsDirectory, cfg); err != nil {
		return fmt.Errorf("writing config files: %w", err)
	}

	return nil
}

// runBasicsForm shows Step 1 — provider, cluster name, and cluster kind.
func runBasicsForm(cfg *PromptedConfig) error {
	const (
		optVPN      = "A new VPN cluster (Phase 0 — hosts Keycloak + NetBird mesh)"
		optWorkload = "A workload cluster (no managed Keycloak; OIDC is optional)"
	)

	clusterKindChoice := optWorkload
	if cfg.ClusterType == constants.ClusterTypeVPN {
		clusterKindChoice = optVPN
	}

	clusterKindGroup := huh.NewGroup(
		huh.NewSelect[string]().
			Title("What are you setting up?").
			Options(
				huh.NewOption(optWorkload, optWorkload),
				huh.NewOption(optVPN, optVPN),
			).
			Value(&clusterKindChoice),
	).WithHideFunc(func() bool {
		// VPN clusters are only supported on Hetzner today.
		return cfg.CloudProvider != constants.CloudProviderHetzner
	})

	err := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Cloud provider:").
				Options(
					huh.NewOption(constants.CloudProviderAWS, constants.CloudProviderAWS),
					huh.NewOption(constants.CloudProviderAzure, constants.CloudProviderAzure),
					huh.NewOption(constants.CloudProviderHetzner, constants.CloudProviderHetzner),
					huh.NewOption(constants.CloudProviderBareMetal, constants.CloudProviderBareMetal),
					huh.NewOption(constants.CloudProviderLocal, constants.CloudProviderLocal),
				).
				Value(&cfg.CloudProvider),
			huh.NewInput().
				Title("Cluster name:").
				Value(&cfg.ClusterName).
				Validate(nonEmpty),
		).Title("Cluster basics").Description("Step 1/4"),
		clusterKindGroup,
	).Run()
	if err != nil {
		return err
	}

	if cfg.CloudProvider != constants.CloudProviderHetzner {
		cfg.ClusterType = constants.ClusterTypeWorkload
	} else if clusterKindChoice == optVPN {
		cfg.ClusterType = constants.ClusterTypeVPN
	} else {
		cfg.ClusterType = constants.ClusterTypeWorkload
	}

	return nil
}

// applyVPNDefaults fills NetBirdDNS, ControlPlaneEndpoint, and ACMEEmail from
// KeycloakDNS when those fields are currently empty (first run) or when
// KeycloakDNS has changed (edit loop). On an edit loop the operator may have
// already typed custom values — we don't overwrite non-empty custom values
// unless they look like they were auto-derived from the old KeycloakDNS.
func applyVPNDefaults(cfg *PromptedConfig) {
	base := stripFirstLabel(cfg.KeycloakDNS)
	if base == "" {
		return
	}
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

// runVPNKeycloakForm shows Step 2a — Keycloak mode and DNS.
func runVPNKeycloakForm(cfg *PromptedConfig) error {
	const (
		optManaged  = "managed (kubeaid-cli installs Keycloak on this cluster)"
		optExternal = "external (use my existing Keycloak elsewhere)"
	)

	keycloakModeChoice := optManaged
	if cfg.KeycloakMode == constants.KeycloakModeExternal {
		keycloakModeChoice = optExternal
	}

	fields := []huh.Field{
		huh.NewSelect[string]().
			Title("Keycloak mode:").
			Options(
				huh.NewOption(optManaged, optManaged),
				huh.NewOption(optExternal, optExternal),
			).
			Value(&keycloakModeChoice),
		huh.NewInput().
			Title("Keycloak DNS (e.g. keycloak.vpn.acme.com):").
			Value(&cfg.KeycloakDNS).
			Validate(nonEmpty),
	}

	// Only show the client-secret field when external mode is selected; we
	// can't use WithHideFunc here because it's per-group not per-field, so
	// we collect the secret in a separate one-field form run after mode is known.
	err := huh.NewForm(
		huh.NewGroup(fields...).
			Title("VPN — Keycloak setup").
			Description("Step 2a/4"),
	).Run()
	if err != nil {
		return err
	}

	if keycloakModeChoice == optManaged {
		cfg.KeycloakMode = constants.KeycloakModeManaged
	} else {
		cfg.KeycloakMode = constants.KeycloakModeExternal
	}

	// Collect the external client secret in its own run so it can be
	// shown only when the mode is external — huh has no per-field hide.
	if cfg.KeycloakMode == constants.KeycloakModeExternal {
		return huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("netbird-backend client secret (from your external Keycloak):").
					EchoMode(huh.EchoModePassword).
					Value(&cfg.NetBirdBackendClientSecret).
					Validate(nonEmpty),
			).Title("VPN — external Keycloak secret").Description("Step 2a/4 (cont.)"),
		).Run()
	}

	return nil
}

// runVPNEndpointsForm shows Step 2b — NetBird DNS, CP endpoint, ACME email.
// These are pre-filled by applyVPNDefaults before this form renders.
func runVPNEndpointsForm(cfg *PromptedConfig) error {
	return huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("NetBird Mgmt DNS (e.g. netbird.vpn.acme.com):").
				Value(&cfg.NetBirdDNS).
				Validate(nonEmpty),
			huh.NewInput().
				Title("Control-plane endpoint FQDN (e.g. api.vpn.acme.com):").
				Value(&cfg.ControlPlaneEndpoint).
				Validate(nonEmpty),
			huh.NewInput().
				Title("ACME email for Let's Encrypt (e.g. ops@acme.com):").
				Value(&cfg.ACMEEmail).
				Validate(nonEmpty),
		).Title("VPN — endpoints").Description("Step 2b/4"),
	).Run()
}

// runOIDCForm shows the OIDC group (workload clusters only).
func runOIDCForm(cfg *PromptedConfig) error {
	// Build the issuer and client ID inputs unconditionally; they are
	// shown only when EnableOIDC is true, gated by a separate group with
	// WithHideFunc.
	oidcDetailsGroup := huh.NewGroup(
		huh.NewInput().
			Title("OIDC issuer URL (e.g. https://keycloak.example/realms/clusters):").
			Value(&cfg.OIDCIssuerURL).
			Validate(httpsURL),
		huh.NewInput().
			TitleFunc(func() string {
				return fmt.Sprintf("OIDC client ID for this cluster (e.g. kubernetes-%s):", cfg.ClusterName)
			}, &cfg.ClusterName).
			Value(&cfg.OIDCClientID).
			Validate(nonEmpty),
	).WithHideFunc(func() bool { return !cfg.EnableOIDC })

	return huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Enable OIDC (Keycloak) authentication on kube-apiserver?").
				Value(&cfg.EnableOIDC),
		).Title("OIDC (optional)").Description("Step 2/4"),
		oidcDetailsGroup,
	).Run()
}

// runGitSSHForm shows Step 4 — ArgoCD deploy key, config repo URL, and (when
// no SSH agent is available) a separate Git SSH private key.
//
// kubeaid-cli pushes to the kubeaid-config fork, so the URL must be SSH form —
// the auth resolver in pkg/utils/git/auth.go only speaks SSH (agent or key file),
// not PAT-based HTTPS. The default and description below reflect that constraint.
//
// The deploy key label clarifies it must be read-only (ArgoCD in-cluster clone);
// the optional SSH key label clarifies it needs write access (kubeaid-cli push).
func runGitSSHForm(cfg *PromptedConfig, detected *autoDetectedConfig) error {
	cfg.UseSSHAgent = detected.SSHAgentAvail

	// gitKeyGroup is hidden when an SSH agent is available; only shown when
	// the operator must supply a key file for kubeaid-cli to push with.
	gitKeyGroup := huh.NewGroup(
		huh.NewInput().
			Title("Your SSH private key (with write access to kubeaid-config — used by kubeaid-cli to push):").
			Value(&cfg.SSHKeyPath).
			Validate(validateSSHKeyPath),
	).WithHide(detected.SSHAgentAvail)

	// Pre-fill the SSH key path default.
	if cfg.SSHKeyPath == "" {
		cfg.SSHKeyPath = detectSSHKeyPath()
	}
	if cfg.KubeaidConfigDeployKeyPath == "" {
		cfg.KubeaidConfigDeployKeyPath = detectSSHKeyPath()
	}

	return huh.NewForm(
		huh.NewGroup(
			// ArgoCD deploy key: read-only SSH key for in-cluster clone.
			// MUST NOT have write access — GitHub Deploy Keys are read-only
			// by default, which is the correct posture here.
			huh.NewInput().
				Title("ArgoCD deploy key — read-only SSH key for in-cluster clone (private key file path):").
				Value(&cfg.KubeaidConfigDeployKeyPath).
				Validate(validateSSHKeyPath),
			huh.NewInput().
				Title("KubeAid Config fork URL:").
				Description("SSH form — uses your yubikey via SSH agent, or the SSH key collected below.").
				Value(&cfg.KubeaidConfigForkURL).
				Validate(nonEmpty),
		).Title("Git / SSH").Description("Step 4/4"),
		gitKeyGroup,
	).Run()
}

// runConfirm shows the "Looks good?" confirm and returns the operator's choice.
func runConfirm() (bool, error) {
	var confirmed bool
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Looks good?").
				Value(&confirmed),
		),
	).Run()
	if err != nil {
		return false, err
	}
	return confirmed, nil
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
