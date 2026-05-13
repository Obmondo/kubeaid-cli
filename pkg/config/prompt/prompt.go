// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package prompt

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/charmbracelet/huh"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/giturl"
)

// KubeaidIsSSH reports whether KubeaidForkURL is an SSH-form Git URL.
// Used by general.yaml.tmpl to decide whether to render the kubeaid
// ArgoCD deploy key block — HTTPS public forks need no key, SSH
// forks (private) do.
func (c *PromptedConfig) KubeaidIsSSH() bool {
	return giturl.IsSSH(c.KubeaidForkURL)
}

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

	// Keycloak reference fields. Populated for VPN clusters
	// (always — kubeaid-cli installs or references Keycloak) and
	// for workload clusters that opted into Keycloak login at the
	// OIDC prompt. Render the cluster.keycloak.{mode,dns,realm}
	// block in general.yaml.
	KeycloakMode  string // "managed" | "external"
	KeycloakDNS   string
	KeycloakRealm string

	// VPN-only fields — populated only for cluster.type=vpn.
	NetBirdDNS string
	ACMEEmail  string

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

	// GitKnownHosts holds known_hosts lines captured at prompt time
	// for SSH-form fork URLs whose host isn't already in the
	// embedded common-providers list (github / gitlab / azure /
	// bitbucket). Persisted into git.knownHosts in general.yaml so
	// subsequent kubeaid-cli runs work offline.
	GitKnownHosts []string

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

// exitCleanlyOnAbort detects huh's user-abort sentinel anywhere in
// the wrapped error chain and replaces the noisy multi-frame error
// with a single friendly line, exiting with status 130 (the
// conventional Ctrl+C exit code). Called as a deferred from
// ConfigFromPrompt with a pointer to the named return so it sees
// the final wrapped error post-defer chain.
//
// Non-abort errors fall through unchanged — caller's slog.Error
// chain still applies for those.
func exitCleanlyOnAbort(errPtr *error) {
	if errPtr == nil || *errPtr == nil {
		return
	}
	if !errors.Is(*errPtr, huh.ErrUserAborted) {
		return
	}
	fmt.Fprintln(os.Stderr, "  Cancelled — no config files written.")
	os.Exit(130)
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
func ConfigFromPrompt(configsDirectory string) (returnErr error) {
	// Catch huh's user-abort sentinel at the single ConfigFromPrompt
	// chokepoint so Ctrl+C exits with a friendly one-line message
	// instead of the deeply-wrapped 'Failed preparing config files
	// error=interactive config setup failed: collecting cluster
	// basics: user aborted' chain that bubbles up through Prepare.
	defer exitCleanlyOnAbort(&returnErr)

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

	// Step 0: K8s version profile picker. Replaces today's silent
	// "latest-1 minor" choice with an explicit picker showing four
	// risk profiles (Proven / Balanced / Early Adopter / Bleeding
	// Edge) so the operator can trade off freshness vs. stability.
	// Picker overrides cfg.K8sVersion when the operator picks; on
	// Ctrl+C / no selection / total fallback failure, the silent
	// autodetect default in detected.K8sVersion is preserved.
	pickedK8s, err := pickK8sProfile(detected)
	if err != nil {
		return fmt.Errorf("picking K8s profile: %w", err)
	}
	if pickedK8s != "" {
		cfg.K8sVersion = pickedK8s
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
			cfg.KeycloakRealm = deriveRealmFromDNS(cfg.KeycloakDNS)
			cfg.OIDCIssuerURL = "https://" + cfg.KeycloakDNS + "/realms/" + cfg.KeycloakRealm
			cfg.OIDCClientID = "kubernetes-" + cfg.ClusterName
		} else {
			if err := runWorkloadKeycloakForm(cfg); err != nil {
				return fmt.Errorf("collecting workload Keycloak config: %w", err)
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

	printWorkloadNetBirdNextSteps(cfg)

	return nil
}

// printWorkloadNetBirdNextSteps prints two manual steps the operator
// has to do before `kubeaid-cli bootstrap` can finish on a workload
// cluster that opted into Keycloak (and therefore wants its kube-API
// behind the NetBird mesh). No-op for VPN clusters and for workload
// clusters that opted out of Keycloak — both flows are self-contained.
//
// Manual on purpose (decision: operator generates the setup key in
// the parent NetBird's UI and pastes it into secrets.yaml; kubeaid-cli
// never speaks to the NetBird Mgmt API). Same applies to NetBird group
// ACLs — operator owns the parent NetBird's NBPolicy / group config.
func printWorkloadNetBirdNextSteps(cfg *PromptedConfig) {
	if cfg.ClusterType != constants.ClusterTypeWorkload || !cfg.EnableOIDC {
		return
	}

	// Derive the typical NetBird Mgmt URL from the Keycloak DNS by
	// swapping the leading "keycloak." label for "netbird." — Obmondo's
	// VPN clusters expose both on the same base domain. Fall through
	// to a placeholder if the prefix doesn't match (operator's
	// off-pattern Keycloak DNS).
	netbirdURL := "<your NetBird Mgmt URL>"
	if strings.HasPrefix(cfg.KeycloakDNS, "keycloak.") {
		netbirdURL = "https://netbird." + strings.TrimPrefix(cfg.KeycloakDNS, "keycloak.")
	}

	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "──────────────────────────────────────────────────────────────────")
	fmt.Fprintln(os.Stderr, "  Two manual steps before `kubeaid-cli bootstrap`:")
	fmt.Fprintln(os.Stderr, "──────────────────────────────────────────────────────────────────")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "  1. Generate a NetBird setup key for this cluster's nodes:")
	fmt.Fprintf(os.Stderr, "       %s  →  Setup Keys  →  Create key\n", netbirdURL)
	fmt.Fprintln(os.Stderr, "     Paste the generated value into secrets.yaml under:")
	fmt.Fprintln(os.Stderr, "       netbird:")
	fmt.Fprintln(os.Stderr, "         setupKey: <paste here>")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "  2. Configure NetBird group ACLs so your laptop can reach the new cluster:")
	fmt.Fprintf(os.Stderr, "       In %s, ensure a NBPolicy lets your laptop's group reach\n", netbirdURL)
	fmt.Fprintf(os.Stderr, "       the cluster peer (typically the group %q) on TCP 6443.\n", "k8s-"+cfg.ClusterName)
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "──────────────────────────────────────────────────────────────────")
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

// runWorkloadKeycloakForm shows the workload-cluster Keycloak group
// (Step 2c). Operator picks whether to wire OIDC at all; if yes,
// supplies the parent Keycloak's DNS / realm / client ID. kubeaid-cli
// then probes the realm's discovery endpoint to catch typos and
// NetBird-down cases before bootstrap kicks off.
//
// When the operator opts out (use admin.conf only), no keycloak block
// is rendered into general.yaml and the bootstrap prints a warning
// that sharing admin.conf isn't best practice.
//
// The kubernetes-<cluster> OIDC client is the operator's responsibility
// to create in the referenced Keycloak (public PKCE, redirect URIs
// http://localhost:8000 + http://localhost:18000). The probe only
// verifies the realm is reachable; it doesn't check the client exists.
func runWorkloadKeycloakForm(cfg *PromptedConfig) error {
	if err := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Authenticate kubectl users via Keycloak (OIDC)?").
				Description("Recommended for shared clusters. Choosing No falls back to admin.conf, which all users share — fine for solo work, not for production.").
				Affirmative("Yes — use Keycloak").
				Negative("No — admin.conf only").
				Value(&cfg.EnableOIDC),
		).Title("OIDC (optional)").Description("Step 2c/4"),
	).Run(); err != nil {
		return err
	}

	if !cfg.EnableOIDC {
		cfg.KeycloakDNS = ""
		cfg.KeycloakRealm = ""
		cfg.OIDCIssuerURL = ""
		cfg.OIDCClientID = ""
		return nil
	}

	if cfg.OIDCClientID == "" {
		cfg.OIDCClientID = "kubernetes-" + cfg.ClusterName
	}
	cfg.KeycloakMode = constants.KeycloakModeExternal

	// Form + probe loop. Each iteration re-runs the form pre-filled
	// with the last values so an operator who fat-fingered a name
	// can fix it without retyping the rest.
	for {
		if err := huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("Keycloak DNS (e.g. keycloak.vpn.acme.com):").
					Value(&cfg.KeycloakDNS).
					Validate(nonEmpty),
				huh.NewInput().
					Title("Keycloak realm (blank = derive from DNS via publicsuffix):").
					Value(&cfg.KeycloakRealm),
				huh.NewInput().
					TitleFunc(func() string {
						return fmt.Sprintf("OIDC client ID (must exist as a public PKCE client in the realm, e.g. kubernetes-%s):", cfg.ClusterName)
					}, &cfg.ClusterName).
					Value(&cfg.OIDCClientID).
					Validate(nonEmpty),
			).Title("Keycloak for workload OIDC").Description("Step 2c/4"),
		).Run(); err != nil {
			return err
		}

		if strings.TrimSpace(cfg.KeycloakRealm) == "" {
			cfg.KeycloakRealm = deriveRealmFromDNS(cfg.KeycloakDNS)
		}

		probeErr := probeOIDCIssuer(context.Background(), cfg.KeycloakDNS, cfg.KeycloakRealm)
		if probeErr == nil {
			cfg.OIDCIssuerURL = "https://" + cfg.KeycloakDNS + "/realms/" + cfg.KeycloakRealm
			return nil
		}

		// Probe failed — show the operator what we saw and let them
		// choose between retrying (loop runs again, form pre-filled)
		// and skipping OIDC entirely.
		retry := true
		if err := huh.NewForm(
			huh.NewGroup(
				huh.NewNote().
					Title("Cannot reach Keycloak").
					Description(renderProbeOIDCError(probeErr)),
				huh.NewConfirm().
					Title("Try again?").
					Affirmative("Edit and retry").
					Negative("Skip OIDC (use admin.conf)").
					Value(&retry),
			),
		).Run(); err != nil {
			return err
		}

		if !retry {
			cfg.EnableOIDC = false
			cfg.KeycloakDNS = ""
			cfg.KeycloakRealm = ""
			cfg.KeycloakMode = ""
			cfg.OIDCIssuerURL = ""
			cfg.OIDCClientID = ""
			return nil
		}
	}
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

	if err := huh.NewForm(
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
				Validate(sshGitURL),
		).Title("Git / SSH").Description("Step 4/4"),
		gitKeyGroup,
	).Run(); err != nil {
		return err
	}

	// Auto-populate git.knownHosts for self-hosted forge URLs whose
	// host keys aren't shipped in the embedded common-providers
	// list. Silent for HTTPS / public-forge URLs.
	populateGitKnownHosts(cfg)

	return nil
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
