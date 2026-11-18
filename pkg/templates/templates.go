// Copyright 2025 Obmondo
// SPDX-License-Identifier: Apache-2.0

// Package templates renders a cluster's KubeAid Config files — the capi-cluster Helm values, the
// ArgoCD Apps and their values, the Sealed Secret sources — from the embedded template tree and
// a config.Config. It does no I/O: whatever the templates need that the config doesn't hold, the
// caller looks up and passes in Inputs.
package templates

import (
	"embed"
	"errors"
	"net/url"
	"path"
	"slices"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	templateUtils "github.com/Obmondo/kubeaid-cli/pkg/utils/templates"
)

//go:embed templates/*
var KubeaidConfigFileTemplates embed.FS

// Render executes the embedded template called name — its path under templates/, such as
// "argocd-apps/values-capi-cluster.yaml.tmpl" — against values.
func Render(name string, values any) ([]byte, error) {
	return templateUtils.ParseAndExecuteTemplate(&KubeaidConfigFileTemplates, path.Join("templates/", name), values)
}

type TemplateValues struct {
	GeneralConfigFileContents string

	config.GitConfig
	config.ForksConfig

	config.ClusterConfig
	*config.KubePrometheusConfig
	CAPIClusterNamespace string

	AWSConfig      *config.AWSConfig
	AWSCredentials *config.AWSCredentials
	AWSB64EncodedCredentials,
	AWSAccountID string

	AzureConfig      *config.AzureConfig
	AzureCredentials *config.AzureCredentials
	CAPIUAMIClientID,
	VeleroUAMIClientID,
	AzureStorageAccountAccessKey,
	ServiceAccountIssuerURL string

	HetznerConfig      *config.HetznerConfig
	HetznerCredentials *config.HetznerCredentials

	// HetznerBareMetalHostPublicIPs maps each HetznerBareMetalHost
	// ServerID to its Robot main IP. Populated at render time via
	// Hetzner.GetHetznerBareMetalHostPublicIPs (Robot API call once
	// per setup-cluster run). Empty for non-bare-metal Hetzner
	// clusters and for non-Hetzner clouds. Consumed by
	// values-kubelet-csr-approver.yaml.tmpl to widen the CSR
	// allow-list with one /32 per node.
	HetznerBareMetalHostPublicIPs map[string]string

	BareMetalConfig *config.BareMetalConfig

	/*
		There are scenarios when we know the control-plane endpoint before the cluster is provisioned :

		  (1) When provisioning an HCloud / Hetzner hybrid cluster, and we have a VPN cluster.

		  (2) When provisioning a Bare Metal / Hetzner Bare Metal cluster; the user specifies it.

		For Hetzner HCloud / hybrid VPN clusters, the endpoint is either the pre-provisioned
		LB private IP, or a configured hostname. When a hostname is configured, kubeaid-cli
		renders the hostname and manages bootstrap/private DNS mapping separately.

		Otherwise, we need to wait until the cluster has been provisioned. Once the cluster is
		provisioned, we get the control-plane endpoint from the Cluster resource. And then it's
		specified to Cilium.

		NOTE : Initially Cilium is installed using the postKubeadm hook in the KubeadmControlPlane
		       resource. The control-plane endpoint is determined from the kubeconfig file in the node.
	*/
	ControlPlaneEndpoint string

	// ControlPlaneEndpointPort is the apiserver port Cilium's kube-proxy
	// replacement dials. Kubeadm based clusters listen on 6443; EKS control
	// planes listen on 443. Populated from the main cluster kubeconfig once
	// the cluster exists, "6443" otherwise.
	ControlPlaneEndpointPort string

	// ControlPlaneLBPrivateIP and ControlPlaneLBBootstrapPublicIP
	// are the HCloud load-balancer's private (steady-state) and
	// bootstrap-only public IPs. Populated only on HCloud-VPN
	// clusters where a control-plane endpoint FQDN is configured;
	// the CoreDNS ConfigMap renders both as A records for the
	// endpoint so resolution works during the bootstrap window
	// (public IP) and after NetBird is up (private IP through the
	// mesh).
	ControlPlaneLBPrivateIP         string
	ControlPlaneLBBootstrapPublicIP string

	// CoturnFloatingIPs are the HCloud Floating IP(s) provisioned for
	// NetBird Coturn (STUN/TURN) HA on a multi-CP HCloud VPN cluster,
	// rendered into the capi-cluster chart's controlPlane.hcloud.floatingIPs
	// so each control-plane node binds them via netplan. Empty otherwise.
	CoturnFloatingIPs []string

	// HCloudSingleNodePublic mirrors config.HCloudSingleNodePublic. Gates the
	// single-node chart values: network.type=public, CCM networking off,
	// apiserver endpoint from the operator's api DNS name.
	HCloudSingleNodePublic bool

	// ControlPlaneExtraCertSANs are operator-supplied extra DNS names rendered
	// into the chart's values so kubeadm includes them in the apiserver TLS
	// cert SAN list alongside the primary endpoint.host. Sourced from
	// controlPlane.extraCertSANs in general.yaml.
	ControlPlaneExtraCertSANs []string

	ExtraKnownHosts []string

	*config.DisasterRecoveryConfig

	*config.ObmondoConfig

	// Subject CN of the Obmondo-issued mTLS cert (ObmondoConfig.CertPath),
	// populated when Obmondo.Monitoring is true. Used in
	// cluster-vars.jsonnet.tmpl as the required `certname` field.
	ObmondoCertCN string

	// Raw file contents of ObmondoConfig.CertPath / KeyPath, populated when
	// Obmondo.Monitoring is true. Base64-encoded into the obmondo-clientcert
	// sealed-secret templates.
	ObmondoCertFileContents string
	ObmondoKeyFileContents  string

	// KeycloakAdminPassword is the plaintext password templated into
	// the keycloak-admin SealedSecret. Populated only when
	// config.ManagedKeycloakEnabled.
	KeycloakAdminPassword string

	// NetBirdBackendClientSecret is the pre-generated OIDC client
	// secret for the `netbird-backend` confidential client. The
	// same value is templated into the netbird SealedSecret AND
	// passed through to ReconcileClient as spec.Secret so Keycloak
	// stores what NetBird's chart already expects in the cluster
	// Secret — single git push, single sync.
	NetBirdBackendClientSecret string

	// Random keys read-or-generated for the netbird Secret on
	// managed-Keycloak VPN clusters. Each is persisted in the
	// in-cluster Secret so re-runs converge to the same value.
	//   DatastoreKey  base64(32 bytes) -> NetBird Mgmt's AES key
	//   RelayPassword alphanumeric     -> Relay shared secret
	//   TurnPassword  alphanumeric     -> matches TURN auth on
	//                                     both Mgmt and Coturn
	NetBirdDatastoreKey  string
	NetBirdRelayPassword string
	NetBirdTurnPassword  string

	// Constant client IDs the gocloak reconciler creates in the
	// realm. Surfaced to templates so the netbird Secret renders
	// the same identifiers without hardcoding strings in YAML.
	NetBirdClientID        string
	NetBirdBackendClientID string

	// NetBirdPostgresDSN is the libpq URI Mgmt uses to connect to
	// the CNPG-managed Postgres. Empty on the very first render
	// (CNPG hasn't generated the password yet); patched into the
	// in-cluster Secret post-sync, then read-back here on
	// subsequent runs so the SealedSecret in git stays correct.
	NetBirdPostgresDSN string

	// NetBirdManagementURL is the NetBird Mgmt endpoint the
	// netbird-operator targets — without it the operator binary
	// defaults to NetBird Cloud (api.netbird.io), which is never
	// right for kubeaid clusters. cluster.netbird.dns on VPN
	// clusters (they host Mgmt themselves); derived via the
	// netbird.<base> Keycloak-DNS convention on workload clusters.
	// Empty when underivable — the values overlay then omits
	// managementURL and the operator must be wired manually.
	NetBirdManagementURL string

	// NetBird surfaces the cluster's netbird config to the
	// values-netbird-operator overlay (used for the clusterProxy block).
	// Nil when the cluster has no netbird block; the flags below are
	// precomputed at render time so the template stays nil-safe.
	NetBird                    *config.NetBirdConfig
	NetBirdClusterProxyEnabled bool
	// NetBirdOperatorEnabled gates the whole `netbird-operator:` overlay
	// (rendered only when this cluster runs the netbird-operator).
	NetBirdOperatorEnabled bool
	// NetBirdRouterEnabled is true when a mesh DNS zone is set
	// (cluster.netbird.dnsZone): the network router and the traefik-internal
	// networkResource are then rendered, using that zone as the router's
	// dnsZoneRef. The chart hard-requires the zone, so the router is only
	// emitted when we have one.
	NetBirdRouterEnabled bool
	// NetBirdAPIKey is secrets.yaml's netbird.apiKey (a Mgmt
	// service-user access token), sealed into the
	// netbird/netbird-mgmt-api-key Secret the operator reads.
	// Empty when the operator hasn't minted one yet — the matching
	// SealedSecret template is skipped and bootstrap pauses at
	// netbird.AwaitOperatorToken instead.
	NetBirdAPIKey string

	// CloudflareAPIToken is secrets.yaml's acme.cloudflareApiToken,
	// sealed into the cert-manager/cloudflare-api-token Secret the
	// DNS-01 ClusterIssuer's solver references. Only consumed when
	// cluster.acmeDNS01 is set (parser validation requires the token
	// by then).
	CloudflareAPIToken string

	// KubeaidStoragectlVersion is the pinned kubeaid-storagectl release
	// tag rendered into global.kubeaidStoragectl.version in the
	// capi-cluster Helm values. Empty for dev/local builds so the chart
	// falls back to its own `latest` logic; set to Inputs.CLIVersion
	// for release builds so each bare-metal node downloads the storagectl
	// binary that matches the kubeaid-cli release that bootstrapped it.
	KubeaidStoragectlVersion string

	// HetznerBareMetalFirewallEnabled is true when the cluster is a Hetzner
	// bare-metal deployment and firewall.enabled is not explicitly false.
	// Computed at render time so the cilium values template can gate the
	// hostNetworkPolicy block without dereferencing the *bool inline.
	HetznerBareMetalFirewallEnabled bool
}

// Inputs is what rendering needs beyond the config: facts the caller looks up from the cloud, the
// cluster or the file-system. Each field is read only by the clusters it concerns, and ignored by
// the rest.
type Inputs struct {
	// GeneralConfigFileContents is the cluster's raw general.yaml, kept in its KubeAid Config
	// copy.
	GeneralConfigFileContents []byte

	// CLIVersion is the release of the binary doing the provisioning. Empty or "dev" means an
	// unreleased build.
	CLIVersion string

	AWS AWSInputs

	Azure AzureInputs

	// HetznerBareMetalHostPublicIPs maps each HetznerBareMetalHost ServerID to its Robot main IP;
	// see Hetzner.GetHetznerBareMetalHostPublicIPs. Needed by Hetzner bare-metal clusters.
	HetznerBareMetalHostPublicIPs map[string]string

	// ObmondoCert is the Obmondo-issued mTLS certificate and key. Required when
	// GeneralConfig.ObmondoIntegrationEnabled.
	ObmondoCert *ObmondoCert

	// NetBirdPostgresDSN is the DSN CNPG generated for NetBird's Postgres, read back from the
	// cluster. Empty until CNPG has generated it.
	NetBirdPostgresDSN string

	// MainClusterEndpoint is the main cluster's API server endpoint, once it is provisioned.
	// Needed by clusters whose endpoint the config doesn't spell out (local, AWS, Azure).
	MainClusterEndpoint *url.URL
}

// AWSInputs are the AWS facts rendering needs.
type AWSInputs struct {
	AccountID string

	// B64EncodedCredentials is the clusterawsadm-encoded credentials CAPA runs with.
	B64EncodedCredentials string
}

// AzureInputs are the Azure facts rendering needs, known once the workload-identity
// infrastructure is provisioned.
type AzureInputs struct {
	CAPIUAMIClientID,
	VeleroUAMIClientID,
	StorageAccountAccessKey,
	ServiceAccountIssuerURL string
}

// ObmondoCert is the Obmondo-issued mTLS certificate and key of a cluster pushing to Obmondo.
type ObmondoCert struct {
	// CN is the Subject CN of the certificate.
	CN string

	// CertFileContents and KeyFileContents are the PEM files, as they are on disk.
	CertFileContents string
	KeyFileContents  string
}

// operatorStoragectlVersionOverride returns the operator-set value of
// general.yaml's `kubeaidStoragectl.version`, or "" when the block is
// omitted entirely. Nil-safe by design — most general.yaml files don't
// carry the block, and reaching through the pointer chain unguarded
// would panic on a perfectly valid config.
func operatorStoragectlVersionOverride(general *config.GeneralConfig) string {
	cfg := general.KubeaidStoragectl
	if cfg == nil {
		return ""
	}
	return cfg.Version
}

// storagectlVersion returns the kubeaid-storagectl version string to
// pin in the capi-cluster Helm values, in priority order:
//
//  1. Operator override from general.yaml's
//     `kubeaidStoragectl.version`. Wins over everything when set —
//     lets an operator pin against a tag newer/older than kubeaid-cli
//     for testing a fix, roll back without re-releasing kubeaid-cli,
//     or point at an unreleased dev build during `go run` bootstrap.
//  2. kubeaid-cli's own version. The default and almost-
//     always-right answer — every node downloads the storagectl that
//     ships with the release that bootstrapped it.
//  3. Empty string when neither is meaningful (dev builds with no
//     override). The chart's preKubeadm template then takes its
//     `{{ else }}latest{{ end }}` branch.
func storagectlVersion(operatorOverride, cliVersion string) string {
	if operatorOverride != "" {
		return operatorOverride
	}
	if cliVersion == "" || cliVersion == "dev" {
		return ""
	}
	return cliVersion
}

// NewTemplateValues returns the values the templates render against for the cluster cfg
// describes, with the lookups of in folded in.
func NewTemplateValues(cfg *config.Config, in Inputs) (*TemplateValues, error) {
	if cfg == nil || cfg.General == nil || cfg.Secrets == nil {
		return nil, errors.New("a config with general and secrets configs is required")
	}
	general, secrets := cfg.General, cfg.Secrets

	// Precompute netbird-operator overlay flags so the values template
	// stays nil-safe (see values-netbird-operator.yaml.tmpl).
	netbirdCfg := general.Cluster.NetBird
	// Router (+ traefik-internal resource) needs the mesh DNS zone the chart
	// requires; only render it when cluster.netbird.dnsZone is set.
	netbirdRouterEnabled := netbirdCfg != nil && netbirdCfg.DNSZone != ""

	templateValues := &TemplateValues{
		GeneralConfigFileContents: string(in.GeneralConfigFileContents),

		GitConfig:   general.Git,
		ForksConfig: general.Forks,

		ClusterConfig:        general.Cluster,
		KubePrometheusConfig: general.KubePrometheus,
		CAPIClusterNamespace: cfg.CAPINamespace(),

		AWSConfig:                general.Cloud.AWS,
		AWSCredentials:           secrets.AWS,
		AWSB64EncodedCredentials: in.AWS.B64EncodedCredentials,
		AWSAccountID:             in.AWS.AccountID,

		AzureConfig:                  general.Cloud.Azure,
		AzureCredentials:             secrets.Azure,
		CAPIUAMIClientID:             in.Azure.CAPIUAMIClientID,
		VeleroUAMIClientID:           in.Azure.VeleroUAMIClientID,
		AzureStorageAccountAccessKey: in.Azure.StorageAccountAccessKey,
		ServiceAccountIssuerURL:      in.Azure.ServiceAccountIssuerURL,

		HetznerConfig:                 sanitizedHetznerConfigForChart(general.Cloud.Hetzner),
		HetznerCredentials:            secrets.Hetzner,
		HCloudSingleNodePublic:        general.HCloudSingleNodePublic(),
		HetznerBareMetalHostPublicIPs: in.HetznerBareMetalHostPublicIPs,

		BareMetalConfig: general.Cloud.BareMetal,

		DisasterRecoveryConfig: general.Cloud.DisasterRecovery,

		ObmondoConfig: general.Obmondo,

		ExtraKnownHosts: general.Git.KnownHosts,

		NetBirdManagementURL: general.NetBirdManagementURL(),
		NetBirdAPIKey:        secrets.NetBirdAPIKey(),

		NetBird:                    netbirdCfg,
		NetBirdClusterProxyEnabled: general.NetBirdClusterProxyEnabled(),
		NetBirdOperatorEnabled:     general.NetBirdOperatorEnabled(),
		NetBirdRouterEnabled:       netbirdRouterEnabled,

		CloudflareAPIToken: cloudflareAPIToken(secrets),

		KubeaidStoragectlVersion: storagectlVersion(
			operatorStoragectlVersionOverride(general),
			in.CLIVersion,
		),

		HetznerBareMetalFirewallEnabled: hetznerBareMetalFirewallEnabled(general),
	}

	// The Subject CN of the Obmondo mTLS cert is required by kube-prometheus's common-template,
	// which fails hard if certname is missing. The cert and key contents feed the
	// obmondo-clientcert sealed-secret templates.
	if general.ObmondoIntegrationEnabled() {
		if in.ObmondoCert == nil {
			return nil, errors.New("the Obmondo certificate is required, since obmondo.monitoring is on")
		}

		templateValues.ObmondoCertCN = in.ObmondoCert.CN
		templateValues.ObmondoCertFileContents = in.ObmondoCert.CertFileContents
		templateValues.ObmondoKeyFileContents = in.ObmondoCert.KeyFileContents
	}

	if general.VPNClusterEnabled() {
		// All NetBird random secrets come from secrets.yaml (auto-
		// generated on first run by parser.FillMissingSecrets, then
		// stable across re-runs). Replaces the prior
		// read-or-generate-from-cluster path that produced spurious
		// SealedSecret diffs whenever the cluster Get failed (timing
		// window before the SealedSecret reconciled, or kubeconfig
		// transiently unreachable).
		netbirdCreds := secrets.NetBird
		if netbirdCreds == nil {
			return nil, errors.New("secrets.netbird is required on a VPN cluster")
		}
		templateValues.NetBirdDatastoreKey = netbirdCreds.DatastoreEncryptionKey
		templateValues.NetBirdRelayPassword = netbirdCreds.RelayPassword
		templateValues.NetBirdTurnPassword = netbirdCreds.TurnPassword

		// postgresDSN is CNPG-generated and only available in-cluster
		// after the netbird-pgsql Cluster CR is reconciled. On the
		// first run the Secret doesn't have the key yet → the caller
		// passes an empty string; the DSN is patched into the Secret
		// post-sync. On re-runs the patched value is read back so the
		// SealedSecret in git stays in sync. This is the only field
		// that genuinely needs a cluster read — we have no way to
		// know CNPG's randomly-generated password ahead of the
		// cluster being up.
		templateValues.NetBirdPostgresDSN = in.NetBirdPostgresDSN

		templateValues.NetBirdClientID = constants.NetBirdClientID
		templateValues.NetBirdBackendClientID = constants.NetBirdBackendClientID

		// netbird-backend OIDC client secret: source-of-truth is
		// secrets.yaml.keycloak.netBirdBackendClientSecret in both
		// modes.
		//   managed:   FillMissingSecrets generated it; the realm
		//              reconciler creates the Keycloak client with
		//              this exact value, and the netbird SealedSecret
		//              renders the same value here.
		//   external:  the operator pre-creates the client in their
		//              external Keycloak and supplies the secret.
		// Either way: one value, one source, no drift.
		if secrets.Keycloak != nil {
			templateValues.NetBirdBackendClientSecret = secrets.Keycloak.NetBirdBackendClientSecret
		}
	}

	if general.ManagedKeycloakEnabled() {
		// Same shape as NetBird above — KeycloakAdminPassword is
		// auto-generated into secrets.yaml on first run and read
		// directly thereafter.
		if secrets.Keycloak == nil {
			return nil, errors.New("secrets.keycloak is required on a VPN cluster with managed Keycloak")
		}
		templateValues.KeycloakAdminPassword = secrets.Keycloak.AdminPassword
	}

	hetznerConfig := templateValues.HetznerConfig

	if err := requireControlPlaneBlocks(cfg); err != nil {
		return nil, err
	}

	// Set the control-plane endpoint.
	switch cfg.CloudProviderName {
	case constants.CloudProviderHetzner:
		switch {
		// Hetzner Bare Metal cluster; the user specifies it.
		case hetznerConfig.Mode == constants.HetznerModeBareMetal:
			templateValues.ControlPlaneEndpoint = hetznerConfig.ControlPlane.BareMetal.Endpoint.Host

		// Single-node public control-plane: no LB — the endpoint is the
		// operator's api DNS name (validated non-empty).
		case general.HCloudSingleNodePublic():
			templateValues.ControlPlaneEndpoint = general.Cloud.Hetzner.ControlPlane.HCloud.LoadBalancer.Endpoint

		// HCloud / Hetzner hybrid clusters where kubeaid-cli pre-
		// provisions the control-plane LB. Endpoint is the hostname
		// when configured (clients resolve via CoreDNS to the LB
		// private IP), else the private IP directly. Two cases land
		// here:
		//   - cluster.type=vpn: this cluster IS the VPN — public LB.
		//   - HCloudVPNCluster set: workload connecting to a VPN —
		//     private LB sitting behind NetBird.
		// Workload clusters not on a VPN don't pre-provision and
		// fall through (CAPI handles their LB lifecycle on its own).
		case (((hetznerConfig.Mode == constants.HetznerModeHCloud) || (hetznerConfig.Mode == constants.HetznerModeHybrid)) && hetznerControlPlaneLBPreProvisioned(general)):
			provisioned := cfg.Provisioned

			templateValues.ControlPlaneLBPrivateIP = provisioned.ControlPlaneLBPrivateIP
			templateValues.ControlPlaneLBBootstrapPublicIP = provisioned.ControlPlaneLBBootstrapPublicIP

			// Coturn Floating IP(s) provisioned in prerequisite-infra for
			// a multi-CP VPN cluster; the chart binds them on each CP via
			// netplan. Empty for every other cluster.
			templateValues.CoturnFloatingIPs = provisioned.CoturnFloatingIPs
			if provisioned.ControlPlaneHostname != "" {
				templateValues.ControlPlaneEndpoint = provisioned.ControlPlaneHostname
				break
			}
			templateValues.ControlPlaneEndpoint = provisioned.ControlPlaneLBPrivateIP
		}

		// Apiserver cert SANs for every Hetzner mode: operator-supplied
		// controlPlane.extraCertSANs (the chart merges these with endpoint.host).
		templateValues.ControlPlaneExtraCertSANs = hetznerControlPlaneCertSANs(hetznerConfig)

	// Bare Metal cluster; the user specifies it.
	case constants.CloudProviderBareMetal:
		templateValues.ControlPlaneEndpoint = general.Cloud.BareMetal.ControlPlane.Endpoint.Host

	default:
		// For local/dev clusters, the main cluster endpoint may not be available yet.
		if endpoint := in.MainClusterEndpoint; endpoint != nil {
			templateValues.ControlPlaneEndpoint = endpoint.Hostname()
			templateValues.ControlPlaneEndpointPort = endpoint.Port()
		}
	}

	// Kubeadm based control planes listen on 6443; the port only differs when
	// the kubeconfig says so (EKS : 443).
	if templateValues.ControlPlaneEndpointPort == "" {
		templateValues.ControlPlaneEndpointPort = "6443"
	}

	return templateValues, nil
}

// hetznerControlPlaneCertSANs returns the operator-supplied extraCertSANs for
// a Hetzner cluster's apiserver cert. The chart merges these with endpoint.host
// into kubeadm's apiServer.certSANs. The mesh-name SAN (kubernetes.<dnsZone>)
// is intentionally NOT added: the NetBird kube-apiserver proxy makes it
// unnecessary.
func hetznerControlPlaneCertSANs(hetznerConfig *config.HetznerConfig) []string {
	return hetznerConfig.ControlPlane.ExtraCertSANs
}

func sanitizedHetznerConfigForChart(hetznerConfig *config.HetznerConfig) *config.HetznerConfig {
	if hetznerConfig == nil {
		return nil
	}

	sanitized := *hetznerConfig
	if hetznerConfig.ControlPlane.HCloud != nil {
		hcloudControlPlane := *hetznerConfig.ControlPlane.HCloud
		hcloudControlPlane.LoadBalancer.Endpoint = ""
		sanitized.ControlPlane.HCloud = &hcloudControlPlane
	}

	return &sanitized
}

// awsNonSecretTemplateNames returns the AWS addon template set — the EKS one
// when the cluster runs a managed control plane.
func awsNonSecretTemplateNames(general *config.GeneralConfig) []string {
	if general.EKSEnabled() {
		return constants.AWSEKSSpecificNonSecretTemplateNames
	}
	return constants.AWSSpecificNonSecretTemplateNames
}

// azureNonSecretTemplateNames returns the Azure addon template set — the AKS
// one when the cluster runs a managed control plane.
func azureNonSecretTemplateNames(general *config.GeneralConfig) []string {
	if general.AKSEnabled() {
		return constants.AzureAKSSpecificNonSecretTemplateNames
	}
	return constants.AzureSpecificNonSecretTemplateNames
}

// requireControlPlaneBlocks fails when the config block that the control-plane endpoint of the
// cloud provider cfg names comes from is not set.
func requireControlPlaneBlocks(cfg *config.Config) error {
	cloud := cfg.General.Cloud

	switch cfg.CloudProviderName {
	case constants.CloudProviderHetzner:
		if cloud.Hetzner == nil {
			return errors.New("cloud.hetzner is required for the Hetzner cloud provider")
		}
		if cloud.Hetzner.Mode == constants.HetznerModeBareMetal && cloud.Hetzner.ControlPlane.BareMetal == nil {
			return errors.New("cloud.hetzner.controlPlane.bareMetal is required in the bare-metal mode")
		}

	case constants.CloudProviderBareMetal:
		if cloud.BareMetal == nil {
			return errors.New("cloud.bare-metal is required for the BareMetal cloud provider")
		}
	}

	return nil
}

func hasGeneralConfig(cfg *config.Config) bool {
	return cfg != nil && cfg.General != nil
}

// NonSecretTemplateNames returns the names of the embedded (non Secret) templates to render for
// the cluster cfg describes, based on its cloud provider and features. The result is the
// caller's to modify. It is nil for a config without a general config.
func NonSecretTemplateNames(cfg *config.Config) []string {
	if !hasGeneralConfig(cfg) {
		return nil
	}
	general := cfg.General

	// Templates common for all cloud providers.
	embeddedTemplateNames := append(constants.CommonNonSecretTemplateNames,
		constants.CommonCloudSpecificNonSecretTemplateNames...,
	)

	// Optional security Apps, opted into via cluster.security. Off by default
	// so an existing cluster's App set does not change on upgrade.
	//
	// trivy-operator and version-checker render together: the chart's
	// ImageOutdatedAndVulnerable alert joins a metric from each.
	if general.VulnerabilityScanningEnabled() {
		embeddedTemplateNames = append(embeddedTemplateNames,
			constants.VulnerabilityScanningTemplateNames...,
		)
	}
	if general.RuntimeDetectionEnabled() {
		embeddedTemplateNames = append(embeddedTemplateNames,
			constants.RuntimeDetectionTemplateNames...,
		)
	}

	// If the user has provided a CA bundle for accessing his / her Git repository,
	// then we need to provide that CA bundle to ArgoCD via a ConfigMap.
	if len(general.Git.CABundle) > 0 {
		embeddedTemplateNames = append(embeddedTemplateNames,
			"argocd-apps/templates/k8s-configs.yaml.tmpl",
			"k8s-configs/argocd-tls-certs-cm.configmap.yaml.tmpl",
		)
	}

	// On HCloud-VPN clusters with a control-plane endpoint, render
	// kube-system/coredns with a hosts block resolving the endpoint
	// to the LB's IPs. ArgoCD owns the ConfigMap; CoreDNS reload
	// picks up edits. Re-adding the k8s-configs App template is
	// safe — duplicate-render is a no-op.
	if hcloudControlPlaneEndpointSet(cfg) {
		embeddedTemplateNames = append(embeddedTemplateNames,
			"argocd-apps/templates/k8s-configs.yaml.tmpl",
			"k8s-configs/coredns.configmap.yaml.tmpl",
		)
	}

	// Add cloud provider specific templates.
	switch cfg.CloudProviderName {
	case constants.CloudProviderAWS:
		embeddedTemplateNames = append(embeddedTemplateNames, awsNonSecretTemplateNames(general)...)

		// Add Disaster Recovery related templates, if the user wants disaster recovery.
		if general.Cloud.DisasterRecovery != nil {
			embeddedTemplateNames = append(embeddedTemplateNames,
				constants.AWSDisasterRecoverySpecificNonSecretTemplateNames...,
			)
		}

	case constants.CloudProviderAzure:
		embeddedTemplateNames = append(embeddedTemplateNames, azureNonSecretTemplateNames(general)...)

		// Add Disaster Recovery related templates, if the user wants disaster
		// recovery. Validation rejects the DR block for AKS clusters.
		if general.Cloud.DisasterRecovery != nil {
			embeddedTemplateNames = append(embeddedTemplateNames,
				constants.AzureDisasterRecoverySpecificNonSecretTemplateNames...,
			)
		}

	case constants.CloudProviderHetzner:
		// CCM selection by mode — mirrors the postKubeadm helm blocks in
		// argocd-helm-charts/capi-cluster/charts/hetzner/templates/KubeadmControlPlane.yaml:
		//   hcloud    → ccm-hcloud only (networking=true, robot=false, owns LBs + routes)
		//   bare-metal → ccm-hetzner only (robot=true, no networking)
		//   hybrid    → both (ccm-hcloud for HCloud nodes + LBs, ccm-hetzner for Robot nodes)
		if general.UsingHCloud() {
			embeddedTemplateNames = append(embeddedTemplateNames,
				constants.HCloudCCMNonSecretTemplateNames...,
			)
		}
		if general.UsingHetznerBareMetal() {
			embeddedTemplateNames = append(embeddedTemplateNames,
				constants.HetznerCCMNonSecretTemplateNames...,
			)
		}

		if general.UsingHetznerBareMetal() {
			embeddedTemplateNames = append(embeddedTemplateNames,
				constants.HetznerBareMetalSpecificNonSecretTemplateNames...,
			)

			// Rook Ceph is rendered only when the cluster has enough bare-metal
			// worker nodes to form a healthy CephCluster (see config.RookCephEnabled).
			//
			// NOTE: if a cluster that already runs Ceph is later re-bootstrapped
			// below the threshold, the rook-ceph App drops out of the rendered set
			// and becomes orphaned from the root App. kubeaid-cli never prunes it
			// automatically (no automated / prune sync policy), but an operator who
			// then deletes it must pass --cascade=false to avoid destroying live
			// Ceph data.
			if general.RookCephEnabled() {
				embeddedTemplateNames = append(embeddedTemplateNames,
					constants.RookCephTemplateNames...,
				)
			}

			// When the control-plane is in Hetzner Bare Metal, and we're using a Failover IP,
			// we need the hetzner-robot ArgoCD App. It'll be responsible for switching the Failover IP
			// to a healthy master node, in a failover scenario.
			if general.ControlPlaneInHetznerBareMetal() &&
				general.Cloud.Hetzner.ControlPlane.BareMetal.Endpoint.IsFailoverIP {

				embeddedTemplateNames = append(embeddedTemplateNames,
					"argocd-apps/templates/hetzner-robot.yaml.tmpl",
					"argocd-apps/values-hetzner-robot.yaml.tmpl",
				)
			}
		}

		if general.UsingHCloud() {
			embeddedTemplateNames = append(embeddedTemplateNames,
				constants.HCloudSpecificNonSecretTemplateNames...,
			)
		}

	case constants.CloudProviderBareMetal:
		embeddedTemplateNames = append(constants.CommonNonSecretTemplateNames,
			constants.BareMetalSpecificNonSecretTemplateNames...,
		)

	case constants.CloudProviderLocal:
		embeddedTemplateNames = constants.CommonNonSecretTemplateNames
	}

	// VPN cluster (any Keycloak mode): traefik for the NetBird Mgmt
	// Ingress (and Keycloak ingress when managed), CloudNativePG
	// for NetBird's Postgres backend, and NetBird Mgmt + Signal +
	// Relay + Dashboard + Coturn themselves. cnpg also backs
	// keycloak-pgsql in managed mode; rendering it here keeps cnpg
	// syncing once regardless of mode.
	if general.VPNClusterEnabled() {
		embeddedTemplateNames = append(embeddedTemplateNames,
			constants.TraefikTemplateNames...,
		)
		embeddedTemplateNames = append(embeddedTemplateNames,
			constants.CloudNativePGTemplateNames...,
		)
		embeddedTemplateNames = append(embeddedTemplateNames,
			constants.NetBirdNonSecretTemplateNames...,
		)
	}

	// netbird-operator app — renders on:
	//   - workload clusters with Keycloak (parent VPN's NetBird Mgmt
	//     is the API target; operator manages NBSetupKey / NBPolicy
	//     and the new NetworkRouter / NetworkResource CRs for the
	//     workload's own peers); and
	//   - VPN clusters (the cluster itself runs NetBird Mgmt — same
	//     operator can drive CRs against the in-cluster API).
	// Values overlay is intentionally empty for now; managementURL /
	// netbirdAPI.keyFromSecret wiring lands in a follow-up (see
	// docs/TODO.md "Wire netbird-operator on both workload and VPN
	// clusters"). Until then the operator pod runs with chart
	// defaults; CRDs are registered and an operator can apply
	// hand-rolled CRs.
	if general.NetBirdOperatorEnabled() {
		embeddedTemplateNames = append(embeddedTemplateNames,
			constants.NetBirdOperatorTemplateNames...,
		)
	}

	// hcloud-fip-controller — multi-CP HCloud VPN cluster only (a Coturn
	// Floating IP was provisioned). Keeps that Floating IP on the active
	// control-plane node so host-network Coturn survives CP failover.
	if general.CoturnFloatingIPEnabled() {
		embeddedTemplateNames = append(embeddedTemplateNames,
			constants.HCloudFIPControllerTemplateNames...,
		)
	}

	// Managed Keycloak only: kubeaid-cli installs the keycloakx
	// chart on this cluster and runs the gocloak realm reconciler
	// post-sync. External-mode VPN clusters skip this — the
	// operator's existing Keycloak handles it.
	if general.ManagedKeycloakEnabled() {
		embeddedTemplateNames = append(embeddedTemplateNames,
			constants.KeycloakManagedNonSecretTemplateNames...,
		)
	}

	// Obmondo customer: include the KubeAid Agent ArgoCD Application
	// templates when monitoring is requested.
	if general.ObmondoIntegrationEnabled() {
		embeddedTemplateNames = append(embeddedTemplateNames,
			constants.KubeAidAgentNonSecretTemplateNames...,
		)
	}

	return slices.Clone(embeddedTemplateNames)
}

// acmeDNS01Enabled reports whether the cluster's ClusterIssuer uses a
// DNS-01 solver — the cluster.acmeDNS01 block is the single switch
// (parser validation guarantees acmeEmail + the provider token exist
// alongside it).
func acmeDNS01Enabled(general *config.GeneralConfig) bool {
	return general.Cluster.ACMEDNS01 != nil
}

// cloudflareAPIToken returns secrets.yaml's acme.cloudflareApiToken,
// nil-safe — the acme block only exists on clusters using DNS-01.
func cloudflareAPIToken(secrets *config.SecretsConfig) string {
	creds := secrets.ACME
	if creds == nil {
		return ""
	}
	return creds.CloudflareAPIToken
}

// hetznerBareMetalFirewallEnabled reports whether the Cilium host-firewall
// hostNetworkPolicy block should be rendered in the cilium values overlay.
// True when all of:
//   - cloud provider is Hetzner bare-metal (HetznerConfig.BareMetal non-nil)
//   - firewall.enabled is nil (default true) or explicitly true
//
// Nil-safe: returns false for any non-Hetzner-bare-metal configuration.
func hetznerBareMetalFirewallEnabled(general *config.GeneralConfig) bool {
	h := general.Cloud.Hetzner
	if h == nil || h.BareMetal == nil {
		return false
	}
	enabled := h.BareMetal.Firewall.Enabled
	return enabled == nil || *enabled
}

// hcloudControlPlaneEndpointSet reports whether kubeaid-cli should
// render the cluster-side coredns-custom ConfigMap for resolving
// the apiserver endpoint. True only when the operator configured
// loadBalancer.endpoint AND the LB has been pre-provisioned (i.e.,
// cfg.Provisioned.ControlPlaneLBPrivateIP is populated). Without the IP, the
// hosts block would render empty and is useless.
func hcloudControlPlaneEndpointSet(cfg *config.Config) bool {
	h := cfg.General.Cloud.Hetzner
	if h == nil || h.ControlPlane.HCloud == nil {
		return false
	}
	if h.ControlPlane.HCloud.LoadBalancer.Endpoint == "" {
		return false
	}
	return cfg.Provisioned.ControlPlaneLBPrivateIP != ""
}

// hetznerControlPlaneLBPreProvisioned reports whether kubeaid-cli
// pre-creates the HCloud control-plane LB (so cfg.Provisioned's
// control-plane LB fields are populated by template-render time). Mirrors the Hetzner-side
// gate in prerequisite_infrastructure.go's shouldPreCreateControlPlaneLB:
//
//	cluster.type=vpn        — this cluster IS the VPN (public LB).
//	HCloudVPNCluster set    — workload connecting to VPN (private LB).
//
// Used in NewTemplateValues to decide whether to populate
// ControlPlaneEndpoint / ControlPlaneLB* from cfg.Provisioned vs. fall through
// to the default (CAPI-managed) path.
func hetznerControlPlaneLBPreProvisioned(general *config.GeneralConfig) bool {
	cluster := general.Cluster
	h := general.Cloud.Hetzner
	if h == nil {
		return false
	}
	return cluster.Type == constants.ClusterTypeVPN || h.HCloudVPNCluster != nil
}

// SecretTemplateNames returns the names of the embedded Secret templates to render for the
// cluster cfg describes, based on its cloud provider and features. The result is the caller's to
// modify. It is nil for a config without a general or a secrets config.
func SecretTemplateNames(cfg *config.Config) []string {
	if cfg == nil || cfg.General == nil || cfg.Secrets == nil {
		return nil
	}
	general, secrets := cfg.General, cfg.Secrets

	// Templates common for all cloud providers.
	embeddedTemplateNames := constants.CommonSecretTemplateNames

	if general.Cluster.ArgoCD.DeployKeys.Kubeaid != nil {
		embeddedTemplateNames = append(embeddedTemplateNames,
			constants.KubeaidDeployKeySecretTemplateName,
		)
	}

	// Add cloud provider specific templates, if required.
	switch cfg.CloudProviderName {
	case constants.CloudProviderAWS:
		embeddedTemplateNames = append(embeddedTemplateNames,
			constants.AWSSpecificSecretTemplateNames...,
		)

	case constants.CloudProviderAzure:
		azureSecretTemplateNames := constants.AzureSpecificSecretTemplateNames
		if general.AKSEnabled() {
			azureSecretTemplateNames = constants.AzureAKSSpecificSecretTemplateNames
		}
		embeddedTemplateNames = append(embeddedTemplateNames, azureSecretTemplateNames...)

		// Add Disaster Recovery related templates, if the user wants disaster
		// recovery. Validation rejects the DR block for AKS clusters.
		if general.Cloud.DisasterRecovery != nil {
			embeddedTemplateNames = append(embeddedTemplateNames,
				constants.AzureDisasterRecoverySpecificSecretTemplateNames...,
			)
		}

	case constants.CloudProviderHetzner:
		embeddedTemplateNames = append(embeddedTemplateNames,
			constants.CommonHetznerSpecificSecretTemplateNames...,
		)

		if general.UsingHetznerBareMetal() {
			embeddedTemplateNames = append(embeddedTemplateNames,
				constants.HetznerBareMetalSpecificSecretTemplateNames...,
			)
		}

	case constants.CloudProviderLocal:
		// No additional provider-specific secret templates needed.
	}

	if general.ObmondoIntegrationEnabled() {
		embeddedTemplateNames = append(embeddedTemplateNames,
			constants.ObmondoClientCertSecretTemplateNames...,
		)
		embeddedTemplateNames = append(embeddedTemplateNames,
			constants.AlertmanagerMainSecretTemplateName,
		)
	}

	// VPN cluster (any Keycloak mode): netbird + netbird-turn-credentials
	// SealedSecrets always. The OIDC client secret inside the
	// netbird Secret is generated by kubeaid-cli when managed,
	// supplied by the operator via secrets.yaml when external.
	if general.VPNClusterEnabled() {
		embeddedTemplateNames = append(embeddedTemplateNames,
			constants.NetBirdSecretTemplateNames...,
		)
	}

	// Managed Keycloak only: keycloak-admin SealedSecret.
	if general.ManagedKeycloakEnabled() {
		embeddedTemplateNames = append(embeddedTemplateNames,
			constants.KeycloakManagedSecretTemplateNames...,
		)
	}

	// netbird-operator Mgmt API token — only when the operator app is
	// rendered AND the operator has minted a service-user token into
	// secrets.yaml. When absent, netbird.AwaitOperatorToken pauses
	// bootstrap with create-it-manually instructions instead of
	// sealing an empty value (which would let the operator pod
	// schedule and then fail at runtime).
	if general.NetBirdOperatorEnabled() && secrets.NetBirdAPIKey() != "" {
		embeddedTemplateNames = append(embeddedTemplateNames,
			constants.NetBirdOperatorAPIKeySecretTemplateName,
		)
	}

	// hcloud-fip-controller token — the HCLOUD_API_TOKEN Secret the
	// controller reads via envFrom. Only when the controller is rendered.
	if general.CoturnFloatingIPEnabled() {
		embeddedTemplateNames = append(embeddedTemplateNames,
			constants.HCloudFIPControllerTokenSecretTemplateName,
		)
	}

	// Cloudflare API token for the DNS-01 ClusterIssuer — gated on the
	// cluster.acmeDNS01 block (parser validation already guarantees the
	// token exists alongside it).
	if acmeDNS01Enabled(general) {
		embeddedTemplateNames = append(embeddedTemplateNames,
			constants.CertManagerCloudflareAPITokenSecretTemplateName,
		)
	}

	return slices.Clone(embeddedTemplateNames)
}
