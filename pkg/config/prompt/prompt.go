// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package prompt

import (
	"bufio"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/huh/spinner"

	configpkg "github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/giturl"
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
	// ConfigsDirectory is the on-disk path the rendered general.yaml
	// and secrets.yaml are written to. Not rendered into the
	// templates — held only so the Hetzner bare-metal add-loop can
	// scan sibling cluster directories for already-used server IDs.
	ConfigsDirectory string

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

	// NetBirdDNSZone is the mesh DNS domain (NetBird --dns-domain), collected
	// for both cluster types (vpn host + workload joiner). Defaults to
	// "<cluster>.local"; written to cluster.netbird.dnsZone.
	NetBirdDNSZone string

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

	// HetznerBMKnownServerIDs is the cached Robot inventory fetched
	// at credential-validation time (on Enter past the password
	// field). Used to seed huh.Input.Suggestions for server-ID
	// autocomplete in the BM add-loop. Transient — not rendered into
	// general.yaml or secrets.yaml.
	HetznerBMKnownServerIDs []string

	// Hetzner bare-metal — populated only when HetznerMode is
	// "bare-metal" (and, in the future, "hybrid" for the BM
	// node-group leg). Lengths line up: CPServerIDs[i] pairs with
	// CPPrivateIPs[i]; same for NodeGroupServerIDs/NodeGroupPrivateIPs.
	HetznerBMCPServerIDs          []string
	HetznerBMCPPrivateIPs         []string
	HetznerBMNodeGroupName        string
	HetznerBMNodeGroupServerIDs   []string
	HetznerBMNodeGroupPrivateIPs  []string
	HetznerBMEndpointHost         string
	HetznerBMEndpointIsFailoverIP bool
	// HetznerBMServerPublicIPs maps a Robot server ID to the public
	// IPv4 the Robot webservice returned for it at validation time.
	// Rendered as a `# id NNN → IP` comment alongside each host in
	// general.yaml so the operator can sanity-check the IDs map to
	// the boxes they expected. Not load-bearing — bootstrap re-reads
	// these via the Robot API at run time.
	HetznerBMServerPublicIPs map[string]string

	// HetznerBMCPRegions is the unique-set of Hetzner region IDs
	// (lower-case, e.g. "fsn1", "hel1", "ash") derived from each
	// chosen control-plane Robot server's DC field. Rendered into
	// global.HetznerConfig.ControlPlane.Regions so the upstream
	// CAPH chart's `minItems: 1` schema check passes — previously
	// bare-metal mode emitted `regions: []` on the theory that
	// kubeaid-cli would fill it from Robot at bootstrap, but the
	// schema validates BEFORE that runtime step ever runs.
	HetznerBMCPRegions []string

	// Hetzner vSwitch — required for hybrid mode (kubeaid-cli's
	// CreateVSwitch is called unconditionally for hybrid) and
	// reserved for the pure-bare-metal auto-create follow-up.
	// Hetzner's webservice rejects VLAN IDs outside 4000-4091, so
	// the prompt validates that range up front.
	HetznerVSwitchName       string
	HetznerVSwitchVLANID     string
	HetznerVSwitchSubnetCIDR string

	// Bare Metal (generic, not Hetzner).
	BareMetalSSHPort      string
	BareMetalEndpointHost string
	BareMetalEndpointPort string

	Obmondo *configpkg.ObmondoConfig
}

var (
	interruptedConfigSaveReader io.Reader = os.Stdin
	interruptedConfigSaveWriter io.Writer = os.Stderr
)

func askSaveInterruptedConfig(configsDirectory string) (bool, error) {
	if _, err := fmt.Fprintf(interruptedConfigSaveWriter,
		"\nSave the answers entered so far to %s so the prompt can resume later? [y/N] ",
		configsDirectory,
	); err != nil {
		return false, fmt.Errorf("writing save prompt: %w", err)
	}

	line, err := bufio.NewReader(interruptedConfigSaveReader).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, fmt.Errorf("reading answer: %w", err)
	}

	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}

// exitCleanlyOnAbort detects huh's user-abort sentinel anywhere in
// the wrapped error chain and replaces the noisy multi-frame error
// with a single resumable-interrupt path, exiting with status 130
// (the conventional Ctrl+C exit code). Called as a deferred from
// ConfigFromPrompt with a pointer to the named return so it sees
// the final wrapped error post-defer chain.
//
// Non-abort errors fall through unchanged — caller's slog.Error
// chain still applies for those.
func exitCleanlyOnAbort(
	errPtr *error,
	configsDirectory string,
	cfg *PromptedConfig,
	state *promptState,
) {
	if errPtr == nil || *errPtr == nil {
		return
	}
	if !errors.Is(*errPtr, huh.ErrUserAborted) {
		return
	}

	save, err := askSaveInterruptedConfig(configsDirectory)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  Cancelled - failed reading save choice: %v\n", err)
		os.Exit(1)
	}
	if !save {
		fmt.Fprintln(os.Stderr, "  Cancelled - partial config not saved.")
		os.Exit(130)
	}

	expandPaths(cfg)
	if err := writeConfigFiles(configsDirectory, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "  Cancelled - failed saving partial config: %v\n", err)
		os.Exit(1)
	}
	if err := writePromptState(configsDirectory, state); err != nil {
		fmt.Fprintf(os.Stderr, "  Cancelled - failed saving prompt state: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "  Saved partial config to %s. Run the command again to continue.\n", configsDirectory)
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
//   - Phase 4: Collect optional Obmondo support details after the summary is accepted.
func ConfigFromPrompt(configsDirectory string) (returnErr error) {
	detected := autoDetect()
	cfg := defaultPromptedConfig(detected)
	cfg.ConfigsDirectory = configsDirectory
	state := &promptState{}
	session := &promptSession{
		configsDirectory: configsDirectory,
		detected:         detected,
		cfg:              cfg,
		state:            state,
	}

	// Catch huh's user-abort sentinel at the single ConfigFromPrompt
	// chokepoint so Ctrl+C exits with a friendly one-line message
	// instead of the deeply-wrapped 'Failed preparing config files
	// error=interactive config setup failed: collecting cluster
	// basics: user aborted' chain that bubbles up through Prepare.
	defer exitCleanlyOnAbort(&returnErr, configsDirectory, cfg, state)

	if err := session.loadExistingConfigIfRequested(); err != nil {
		return err
	}
	if err := session.pickK8sProfileIfNeeded(); err != nil {
		return err
	}
	if err := session.runPromptLoop(); err != nil {
		return err
	}

	// Expand tilde in all file paths so that paths are absolute.
	expandPaths(cfg)

	if err := writeConfigFiles(configsDirectory, cfg); err != nil {
		return fmt.Errorf("writing config files: %w", err)
	}
	if err := removePromptState(configsDirectory); err != nil {
		return fmt.Errorf("removing prompt state: %w", err)
	}

	printWorkloadNetBirdNextSteps(cfg)

	return nil
}

func defaultPromptedConfig(detected *autoDetectedConfig) *PromptedConfig {
	return &PromptedConfig{
		ClusterType:           constants.ClusterTypeWorkload,
		SSHUsername:           "git",
		KubeaidForkURL:        constants.KubeAidPublicHTTPSURL,
		KubeaidConfigForkURL:  "git@github.com:Obmondo/kubeaid-config.git",
		K8sVersion:            detected.K8sVersion,
		KubePrometheusVersion: detected.KubePrometheusVersion,
		KubeaidVersion:        detected.KubeAidVersion,
		HetznerMode:           "hcloud",
		HetznerHCloudZone:     "eu-central",
		HetznerCPMachineType:  "cax21",
		HetznerRegion:         "hel1",
	}
}

type promptSession struct {
	configsDirectory string
	detected         *autoDetectedConfig
	cfg              *PromptedConfig
	state            *promptState
}

func (s *promptSession) loadExistingConfigIfRequested() error {
	if !existingPromptConfigPresent(s.configsDirectory) {
		return nil
	}

	loadExisting, err := confirmLoadExistingConfig(s.configsDirectory)
	if err != nil {
		return fmt.Errorf("confirming existing config load: %w", err)
	}
	if !loadExisting {
		return nil
	}

	loadedState, stateLoaded, err := loadPromptState(s.configsDirectory)
	if err != nil {
		return fmt.Errorf("loading prompt state: %w", err)
	}
	loadedConfig, err := loadExistingPromptedConfigIfPresent(s.configsDirectory, s.cfg)
	if err != nil {
		return fmt.Errorf("loading existing config: %w", err)
	}

	switch {
	case !loadedConfig:
		*s.state = promptState{}
	case stateLoaded:
		*s.state = loadedState
	default:
		*s.state = completedPromptStateFromValues(s.cfg)
	}

	return nil
}

func (s *promptSession) pickK8sProfileIfNeeded() error {
	if s.state.K8sProfile && s.cfg.K8sVersion != "" {
		return nil
	}

	pickedK8s, err := pickK8sProfile(s.detected)
	if err != nil {
		return fmt.Errorf("picking K8s profile: %w", err)
	}
	if pickedK8s != "" {
		s.cfg.K8sVersion = pickedK8s
	}
	s.state.K8sProfile = true

	return nil
}

func (s *promptSession) runPromptLoop() error {
	for {
		if err := s.runPromptSteps(); err != nil {
			return err
		}
		printSummary(s.cfg, s.state)

		confirmed, err := runConfirm()
		if err != nil {
			return fmt.Errorf("confirming config: %w", err)
		}
		if confirmed {
			if err := s.collectObmondoSupportIfNeeded(); err != nil {
				return err
			}
			return nil
		}

		// Operator picked No — loop back; all cfg fields carry the
		// last-entered values so the form reopens pre-filled.
		*s.state = promptState{}
	}
}

func (s *promptSession) runPromptSteps() error {
	if err := s.collectBasicsIfNeeded(); err != nil {
		return err
	}
	if err := s.collectClusterAuthIfNeeded(); err != nil {
		return err
	}

	if err := s.collectNetBirdDNSZoneIfNeeded(); err != nil {
		return err
	}

	prompter := prompterForProvider(s.cfg.CloudProvider)
	if err := s.collectProviderCredentialsIfNeeded(prompter); err != nil {
		return err
	}
	if err := s.collectGitSSHIfNeeded(); err != nil {
		return err
	}

	if aws, ok := prompter.(*awsPrompter); ok {
		aws.postProcess(s.cfg)
	}

	return nil
}

func (s *promptSession) collectBasicsIfNeeded() error {
	if s.state.Basics && !missingBasics(s.cfg) {
		return nil
	}

	if err := runBasicsForm(s.cfg); err != nil {
		return fmt.Errorf("collecting cluster basics: %w", err)
	}
	s.state.Basics = true

	return nil
}

func (s *promptSession) collectClusterAuthIfNeeded() error {
	if s.cfg.ClusterType == constants.ClusterTypeVPN {
		return s.collectVPNConfigIfNeeded()
	}
	return s.collectWorkloadKeycloakIfNeeded()
}

// collectNetBirdDNSZoneIfNeeded asks for the mesh DNS zone (NetBird
// --dns-domain) for every cluster type — vpn host and workload joiner alike.
// The zone is used to create the DNS zone on NetBird and drives --dns-domain;
// it is operator-supplied and required.
func (s *promptSession) collectNetBirdDNSZoneIfNeeded() error {
	if s.state.NetBirdDNSZone && s.cfg.NetBirdDNSZone != "" {
		return nil
	}

	if err := runNetBirdDNSZoneForm(s.cfg); err != nil {
		return fmt.Errorf("collecting NetBird mesh DNS zone: %w", err)
	}
	s.state.NetBirdDNSZone = true

	return nil
}

func (s *promptSession) collectVPNConfigIfNeeded() error {
	if !s.state.VPNKeycloak || missingVPNKeycloak(s.cfg) {
		if err := runVPNKeycloakForm(s.cfg); err != nil {
			return fmt.Errorf("collecting VPN Keycloak setup: %w", err)
		}
		s.state.VPNKeycloak = true
	}

	applyVPNDefaults(s.cfg)

	if !s.state.VPNEndpoints || missingVPNEndpoints(s.cfg) {
		if err := runVPNEndpointsForm(s.cfg); err != nil {
			return fmt.Errorf("collecting VPN endpoints: %w", err)
		}
		s.state.VPNEndpoints = true
	}

	s.cfg.EnableOIDC = true
	s.cfg.KeycloakRealm = deriveRealmFromDNS(s.cfg.KeycloakDNS)
	// /auth/realms matches the keycloakx chart's base path; see
	// parser/keycloak.go's hydrateKeycloakOIDC for the same
	// derivation on the validate-config side.
	s.cfg.OIDCIssuerURL = "https://" + s.cfg.KeycloakDNS + "/auth/realms/" + s.cfg.KeycloakRealm
	s.cfg.OIDCClientID = "kubernetes-" + s.cfg.ClusterName

	return nil
}

func (s *promptSession) collectWorkloadKeycloakIfNeeded() error {
	if s.state.WorkloadKeycloak && !missingWorkloadKeycloak(s.cfg) {
		return nil
	}

	if err := runWorkloadKeycloakForm(s.cfg); err != nil {
		return fmt.Errorf("collecting workload Keycloak config: %w", err)
	}
	s.state.WorkloadKeycloak = true

	return nil
}

func (s *promptSession) collectProviderCredentialsIfNeeded(prompter ProviderPrompter) error {
	if s.state.ProviderCredentials && !missingProviderPromptConfig(s.cfg) {
		return nil
	}

	if err := prompter.RunCredentialsForm(s.cfg, s.detected); err != nil {
		return fmt.Errorf("collecting provider credentials: %w", err)
	}
	s.state.ProviderCredentials = true

	return nil
}

func (s *promptSession) collectGitSSHIfNeeded() error {
	if s.state.GitSSH && !missingGitSSH(s.cfg) {
		return nil
	}

	if err := runGitSSHForm(s.cfg, s.detected); err != nil {
		return fmt.Errorf("collecting Git/SSH config: %w", err)
	}
	s.state.GitSSH = true

	return nil
}

func (s *promptSession) collectObmondoSupportIfNeeded() error {
	if s.state.ObmondoSupport && !missingObmondoSupportConfig(s.cfg) {
		return nil
	}

	if err := runObmondoSupportForm(s.cfg); err != nil {
		return fmt.Errorf("collecting Obmondo support config: %w", err)
	}
	s.state.ObmondoSupport = true

	return nil
}

// printWorkloadNetBirdNextSteps prints two manual steps the operator
// has to do before `kubeaid-cli bootstrap` can finish on a workload
// cluster that opted into Keycloak (and therefore wants its kube-API
// behind the NetBird mesh). No-op for VPN clusters and for workload
// clusters that opted out of Keycloak — both flows are self-contained.
//
// Manual on purpose (decision: the operator mints the service-user
// access token in the parent NetBird's dashboard and pastes it into
// secrets.yaml; kubeaid-cli never speaks to the NetBird Mgmt API with
// admin powers of its own). The token is what the netbird-operator
// authenticates to the Mgmt API with — it mints setup keys for
// routing peers itself, so no manual setup key is involved. Same
// manual-ownership applies to NetBird group ACLs.
//
// An earlier revision of this notice asked for a *setup key* pasted
// under netbird.setupKey — a field that never existed and a
// credential type that can't call the Mgmt API. If the token is left
// blank, bootstrap pauses at awaitNetBirdOperatorToken with the same
// instructions instead.
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

	clusterGroup := "k8s-" + cfg.ClusterName

	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "──────────────────────────────────────────────────────────────────")
	fmt.Fprintln(os.Stderr, "  Two manual steps before `kubeaid-cli bootstrap`:")
	fmt.Fprintln(os.Stderr, "──────────────────────────────────────────────────────────────────")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "  1. Create a NetBird service user + access token for the netbird-operator")
	fmt.Fprintln(os.Stderr, "     (the operator calls the Mgmt API to wire this cluster into the mesh —")
	fmt.Fprintln(os.Stderr, "     it mints setup keys for its routing peers itself):")
	fmt.Fprintf(os.Stderr, "       %s  →  Team  →  Service Users  →  + Create Service User\n", netbirdURL)
	fmt.Fprintln(os.Stderr, "         Name:  k8s-operator")
	fmt.Fprintln(os.Stderr, "         Role:  Admin")
	fmt.Fprintln(os.Stderr, "       From the new user's row  →  ⋮  →  Tokens  →  + Generate Token")
	fmt.Fprintf(os.Stderr, "         Name:        kubeaid-%s\n", cfg.ClusterName)
	fmt.Fprintln(os.Stderr, "         Expiration:  the longest the UI offers (token shows only once)")
	fmt.Fprintln(os.Stderr, "     Paste the generated token into secrets.yaml under:")
	fmt.Fprintln(os.Stderr, "       netbird:")
	fmt.Fprintln(os.Stderr, "         apiKey: <paste here>")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "  2. Configure NetBird group ACLs so your laptop can reach the new cluster:")
	fmt.Fprintf(os.Stderr, "       In %s, ensure a policy lets your laptop's group reach\n", netbirdURL)
	fmt.Fprintf(os.Stderr, "       the cluster's routing peers (typically the group %q) on TCP 6443.\n", clusterGroup)
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

	switch {
	case cfg.CloudProvider != constants.CloudProviderHetzner:
		cfg.ClusterType = constants.ClusterTypeWorkload
	case clusterKindChoice == optVPN:
		cfg.ClusterType = constants.ClusterTypeVPN
	default:
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

// runNetBirdDNSZoneForm asks for the mesh DNS zone (NetBird --dns-domain),
// shown for every cluster type. cfg.NetBirdDNSZone is pre-filled with the
// "<cluster>.local" default so the input shows it; the user accepts or overrides.
func runNetBirdDNSZoneForm(cfg *PromptedConfig) error {
	return huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("NetBird mesh DNS zone (e.g. mesh.acme.com):").
				Description("The domain your NetBird mesh resolves peers under (NetBird --dns-domain). Used to create the DNS zone on NetBird. Required.").
				Value(&cfg.NetBirdDNSZone).
				Validate(nonEmpty),
		).Title("NetBird — mesh DNS zone"),
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

		// Visible feedback during the probe — without it the form
		// freezes silently for up to promptOIDCProbeTimeout (10s)
		// when NetBird is down or the realm is unreachable.
		var probeErr error
		_ = spinner.New().
			Title(fmt.Sprintf("  Validating Keycloak at https://%s/auth/realms/%s ...", cfg.KeycloakDNS, cfg.KeycloakRealm)).
			Action(func() {
				probeErr = probeOIDCIssuer(context.Background(), cfg.KeycloakDNS, cfg.KeycloakRealm)
			}).
			Run()

		if probeErr == nil {
			// /auth/realms — keycloakx chart base path; must match
			// the URL probeOIDCIssuer just hit, the JWT `iss` claim
			// Keycloak will emit, and the kube-apiserver
			// AuthenticationConfiguration's jwt[].issuer.url.
			cfg.OIDCIssuerURL = "https://" + cfg.KeycloakDNS + "/auth/realms/" + cfg.KeycloakRealm
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

func runObmondoSupportForm(cfg *PromptedConfig) error {
	obmondoSupport := obmondoSupportEnabled(cfg)
	if err := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Do you want Obmondo support?").
				Affirmative("Yes").
				Negative("No").
				Value(&obmondoSupport),
		).Title("Obmondo support").Description("Step 5/5"),
	).Run(); err != nil {
		return err
	}
	if !obmondoSupport {
		cfg.Obmondo = nil
		return nil
	}

	obmondo := ensureObmondoConfig(cfg)
	obmondo.Monitoring = true

	return huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Obmondo mTLS cert path:").
				Value(&obmondo.CertPath).
				Validate(validateObmondoCertPath),
			huh.NewInput().
				Title("Obmondo mTLS key path:").
				Value(&obmondo.KeyPath).
				Validate(func(keyPath string) error {
					return validateObmondoKeyPath(obmondo.CertPath, keyPath)
				}),
		).Title("Obmondo support details").Description("Step 5/5"),
	).Run()
}

func obmondoSupportEnabled(cfg *PromptedConfig) bool {
	return cfg.Obmondo != nil && cfg.Obmondo.Monitoring
}

func ensureObmondoConfig(cfg *PromptedConfig) *configpkg.ObmondoConfig {
	if cfg.Obmondo == nil {
		cfg.Obmondo = &configpkg.ObmondoConfig{}
	}
	return cfg.Obmondo
}

// runConfirm shows the "Looks good?" confirm and returns the operator's choice.
func runConfirm() (bool, error) {
	confirmed := true
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
