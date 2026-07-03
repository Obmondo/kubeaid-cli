// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package core

import (
	"context"
	"embed"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/Obmondo/kubeaid-cli/pkg/cert"
	"github.com/Obmondo/kubeaid-cli/pkg/cloud/aws"
	"github.com/Obmondo/kubeaid-cli/pkg/cloud/azure"
	"github.com/Obmondo/kubeaid-cli/pkg/cloud/hetzner"
	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	corenetbird "github.com/Obmondo/kubeaid-cli/pkg/core/netbird"
	"github.com/Obmondo/kubeaid-cli/pkg/globals"
	"github.com/Obmondo/kubeaid-cli/pkg/netbird"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/kubernetes"
)

//go:embed templates/*
var KubeaidConfigFileTemplates embed.FS

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
	// NetBirdInternalIngressGroup is the NetBird group the traefik-internal
	// networkResource is bound to (k8s-<cluster.name>); the operator creates
	// it in the NetBird dashboard during bootstrap.
	NetBirdInternalIngressGroup string

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
	// falls back to its own `latest` logic; set to globals.KubeaidCLIVersion
	// for release builds so each bare-metal node downloads the storagectl
	// binary that matches the kubeaid-cli release that bootstrapped it.
	KubeaidStoragectlVersion string

	// HetznerBareMetalFirewallEnabled is true when the cluster is a Hetzner
	// bare-metal deployment and firewall.enabled is not explicitly false.
	// Computed at render time so the cilium values template can gate the
	// hostNetworkPolicy block without dereferencing the *bool inline.
	HetznerBareMetalFirewallEnabled bool
}

// operatorStoragectlVersionOverride returns the operator-set value of
// general.yaml's `kubeaidStoragectl.version`, or "" when the block is
// omitted entirely. Nil-safe by design — most general.yaml files don't
// carry the block, and reaching through the pointer chain unguarded
// would panic on a perfectly valid config.
func operatorStoragectlVersionOverride() string {
	cfg := config.ParsedGeneralConfig.KubeaidStoragectl
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
//  2. kubeaid-cli's own KubeaidCLIVersion. The default and almost-
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

func getTemplateValues(ctx context.Context) *TemplateValues {
	// Precompute netbird-operator overlay flags so the values template
	// stays nil-safe (see values-netbird-operator.yaml.tmpl).
	netbirdCfg := config.ParsedGeneralConfig.Cluster.NetBird
	netbirdMgmtURL := corenetbird.ManagementURL()
	netbirdProxyEnabled := corenetbird.ClusterProxyEnabled()
	// Router (+ traefik-internal resource) needs the mesh DNS zone the chart
	// requires; only render it when cluster.netbird.dnsZone is set.
	netbirdRouterEnabled := netbirdCfg != nil && netbirdCfg.DNSZone != ""

	templateValues := &TemplateValues{
		GeneralConfigFileContents: string(config.GeneralConfigFileContents),

		GitConfig:   config.ParsedGeneralConfig.Git,
		ForksConfig: config.ParsedGeneralConfig.Forks,

		ClusterConfig:        config.ParsedGeneralConfig.Cluster,
		KubePrometheusConfig: config.ParsedGeneralConfig.KubePrometheus,
		CAPIClusterNamespace: kubernetes.GetCapiClusterNamespace(),

		AWSConfig:      config.ParsedGeneralConfig.Cloud.AWS,
		AWSCredentials: config.ParsedSecretsConfig.AWS,

		AzureConfig:                  config.ParsedGeneralConfig.Cloud.Azure,
		AzureCredentials:             config.ParsedSecretsConfig.Azure,
		CAPIUAMIClientID:             globals.CAPIUAMIClientID,
		VeleroUAMIClientID:           globals.VeleroUAMIClientID,
		AzureStorageAccountAccessKey: globals.AzureStorageAccountAccessKey,

		HetznerConfig:          sanitizedHetznerConfigForChart(config.ParsedGeneralConfig.Cloud.Hetzner),
		HetznerCredentials:     config.ParsedSecretsConfig.Hetzner,
		HCloudSingleNodePublic: config.HCloudSingleNodePublic(),

		BareMetalConfig: config.ParsedGeneralConfig.Cloud.BareMetal,

		DisasterRecoveryConfig: config.ParsedGeneralConfig.Cloud.DisasterRecovery,

		ObmondoConfig: config.ParsedGeneralConfig.Obmondo,

		ExtraKnownHosts: config.ParsedGeneralConfig.Git.KnownHosts,

		NetBirdManagementURL: netbirdMgmtURL,
		NetBirdAPIKey:        corenetbird.APIKey(),

		NetBird:                     netbirdCfg,
		NetBirdClusterProxyEnabled:  netbirdProxyEnabled,
		NetBirdOperatorEnabled:      corenetbird.OperatorEnabled(),
		NetBirdRouterEnabled:        netbirdRouterEnabled,
		NetBirdInternalIngressGroup: "k8s-" + config.ParsedGeneralConfig.Cluster.Name,

		CloudflareAPIToken: cloudflareAPIToken(),

		KubeaidStoragectlVersion: storagectlVersion(
			operatorStoragectlVersionOverride(),
			globals.KubeaidCLIVersion,
		),

		HetznerBareMetalFirewallEnabled: hetznerBareMetalFirewallEnabled(),
	}

	// Populate Hetzner bare-metal host public IPs via Robot API for the
	// kubelet-csr-approver values template. Empty map for non-Hetzner
	// or non-bare-metal setups (the values template guards on the map
	// being non-empty before rendering its per-host /32 entries).
	if globals.CloudProviderName == constants.CloudProviderHetzner {
		if hetznerProvider, ok := globals.CloudProvider.(*hetzner.Hetzner); ok {
			publicIPs, err := hetznerProvider.GetHetznerBareMetalHostPublicIPs(ctx)
			assert.AssertErrNil(ctx, err,
				"Failed fetching Hetzner bare-metal host public IPs from Robot API")
			templateValues.HetznerBareMetalHostPublicIPs = publicIPs
		}
	}

	// Extract the Subject CN from the Obmondo mTLS cert when monitoring is on.
	// kube-prometheus's common-template fails hard if certname is missing.
	// Also load the cert + key file contents so the obmondo-clientcert
	// sealed-secret templates can base64-encode them. Paths are validated by
	// validateConfigs — re-read here to fail with context if they became
	// unreadable between parse and render.
	if config.ParsedGeneralConfig.Obmondo != nil && config.ParsedGeneralConfig.Obmondo.Monitoring {
		obmondo := config.ParsedGeneralConfig.Obmondo

		cn, certErr := cert.ReadCN(obmondo.CertPath)
		assert.AssertErrNil(ctx, certErr, "Failed reading Obmondo cert CN",
			slog.String("path", obmondo.CertPath))
		templateValues.ObmondoCertCN = cn

		certData, err := os.ReadFile(obmondo.CertPath)
		assert.AssertErrNil(ctx, err,
			"Failed reading Obmondo cert file",
			slog.String("path", obmondo.CertPath))
		templateValues.ObmondoCertFileContents = string(certData)

		key, err := os.ReadFile(obmondo.KeyPath)
		assert.AssertErrNil(ctx, err,
			"Failed reading Obmondo key file",
			slog.String("path", obmondo.KeyPath))
		templateValues.ObmondoKeyFileContents = string(key)
	}

	if config.VPNClusterEnabled() {
		// All NetBird random secrets come from secrets.yaml (auto-
		// generated on first run by parser.FillMissingSecrets, then
		// stable across re-runs). Replaces the prior
		// read-or-generate-from-cluster path that produced spurious
		// SealedSecret diffs whenever the cluster Get failed (timing
		// window before the SealedSecret reconciled, or kubeconfig
		// transiently unreachable).
		netbirdCreds := config.ParsedSecretsConfig.NetBird
		templateValues.NetBirdDatastoreKey = netbirdCreds.DatastoreEncryptionKey
		templateValues.NetBirdRelayPassword = netbirdCreds.RelayPassword
		templateValues.NetBirdTurnPassword = netbirdCreds.TurnPassword

		// postgresDSN is CNPG-generated and only available in-cluster
		// after the netbird-pgsql Cluster CR is reconciled. On the
		// first kubeaid-cli run the Secret doesn't have the key yet
		// → render an empty string; bootstrap_cluster.go's
		// netbird.WaitAndPatchPostgresDSN step fills it in post-sync. On
		// re-runs the patched value is read back here so the
		// SealedSecret in git stays in sync. This is the only field
		// that genuinely needs a cluster read — kubeaid-cli has no
		// way to know CNPG's randomly-generated password ahead of
		// the cluster being up.
		clusterClient, _ := kubernetes.CreateKubernetesClient(ctx,
			constants.OutputPathMainClusterKubeconfig,
		)
		templateValues.NetBirdPostgresDSN = readSecretValueOrEmpty(ctx, clusterClient,
			constants.NamespaceNetBird,
			constants.SecretNameNetBird,
			constants.SecretKeyNetBirdPostgresDSN,
		)

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
		if config.ParsedSecretsConfig.Keycloak != nil {
			templateValues.NetBirdBackendClientSecret = config.ParsedSecretsConfig.Keycloak.NetBirdBackendClientSecret
		}
	}

	if config.ManagedKeycloakEnabled() {
		// Same shape as NetBird above — KeycloakAdminPassword is
		// auto-generated into secrets.yaml on first run and read
		// directly thereafter.
		templateValues.KeycloakAdminPassword = config.ParsedSecretsConfig.Keycloak.AdminPassword
	}

	// Set cloud provider specific values.
	switch globals.CloudProviderName {
	case constants.CloudProviderAWS:
		accountID, accountErr := aws.GetAccountID(ctx)
		assert.AssertErrNil(ctx, accountErr, "Failed getting AWS account ID")

		templateValues.AWSAccountID = accountID
		templateValues.AWSB64EncodedCredentials = os.Getenv(
			constants.EnvNameAWSB64EcodedCredentials,
		)

	case constants.CloudProviderAzure:
		saIssuerURL, saErr := azure.GetServiceAccountIssuerURL()
		assert.AssertErrNil(ctx, saErr, "Failed getting Azure ServiceAccount issuer URL")
		templateValues.ServiceAccountIssuerURL = saIssuerURL
	}

	hetznerConfig := templateValues.HetznerConfig

	// Set the control-plane endpoint.
	switch globals.CloudProviderName {
	case constants.CloudProviderHetzner:
		switch {
		// Hetzner Bare Metal cluster; the user specifies it.
		case hetznerConfig.Mode == constants.HetznerModeBareMetal:
			templateValues.ControlPlaneEndpoint = hetznerConfig.ControlPlane.BareMetal.Endpoint.Host

		// Single-node public control-plane: no LB — the endpoint is the
		// operator's api DNS name (validated non-empty).
		case config.HCloudSingleNodePublic():
			templateValues.ControlPlaneEndpoint = config.ParsedGeneralConfig.Cloud.Hetzner.ControlPlane.HCloud.LoadBalancer.Endpoint

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
		case (((hetznerConfig.Mode == constants.HetznerModeHCloud) || (hetznerConfig.Mode == constants.HetznerModeHybrid)) && hetznerControlPlaneLBPreProvisioned()):
			templateValues.ControlPlaneLBPrivateIP = globals.ControlPlaneLBPrivateIP
			templateValues.ControlPlaneLBBootstrapPublicIP = globals.ControlPlaneLBBootstrapPublicIP

			// Coturn Floating IP(s) provisioned in prerequisite-infra for
			// a multi-CP VPN cluster; the chart binds them on each CP via
			// netplan. Empty for every other cluster.
			templateValues.CoturnFloatingIPs = globals.CoturnFloatingIPs
			if globals.ControlPlaneHostname != "" {
				templateValues.ControlPlaneEndpoint = globals.ControlPlaneHostname
				break
			}
			templateValues.ControlPlaneEndpoint = globals.ControlPlaneLBPrivateIP
		}

		// Apiserver cert SANs for every Hetzner mode: operator-supplied
		// controlPlane.extraCertSANs (the chart merges these with endpoint.host).
		templateValues.ControlPlaneExtraCertSANs = hetznerControlPlaneCertSANs(hetznerConfig)

	// Bare Metal cluster; the user specifies it.
	case constants.CloudProviderBareMetal:
		templateValues.ControlPlaneEndpoint = config.ParsedGeneralConfig.Cloud.BareMetal.ControlPlane.Endpoint.Host

	default:
		// For local/dev clusters, the main cluster endpoint may not be available yet.
		endpoint, endpointErr := kubernetes.GetMainClusterEndpoint(ctx)
		assert.AssertErrNil(ctx, endpointErr, "Failed getting main cluster endpoint")
		if endpoint != nil {
			templateValues.ControlPlaneEndpoint = endpoint.Hostname()
		}
	}

	return templateValues
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

// Returns the list of embedded (non Secret) template names based on the underlying cloud provider.
func getEmbeddedNonSecretTemplateNames() []string {
	// Templates common for all cloud providers.
	embeddedTemplateNames := append(constants.CommonNonSecretTemplateNames,
		constants.CommonCloudSpecificNonSecretTemplateNames...,
	)

	// If the user has provided a CA bundle for accessing his / her Git repository,
	// then we need to provide that CA bundle to ArgoCD via a ConfigMap.
	if len(config.ParsedGeneralConfig.Git.CABundle) > 0 {
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
	if hcloudControlPlaneEndpointSet() {
		embeddedTemplateNames = append(embeddedTemplateNames,
			"argocd-apps/templates/k8s-configs.yaml.tmpl",
			"k8s-configs/coredns.configmap.yaml.tmpl",
		)
	}

	// Add cloud provider specific templates.
	switch globals.CloudProviderName {
	case constants.CloudProviderAWS:
		embeddedTemplateNames = append(embeddedTemplateNames,
			constants.AWSSpecificNonSecretTemplateNames...,
		)

		// Add Disaster Recovery related templates, if the user wants disaster recovery.
		if config.ParsedGeneralConfig.Cloud.DisasterRecovery != nil {
			embeddedTemplateNames = append(embeddedTemplateNames,
				constants.AWSDisasterRecoverySpecificNonSecretTemplateNames...,
			)
		}

	case constants.CloudProviderAzure:
		embeddedTemplateNames = append(embeddedTemplateNames,
			constants.AzureSpecificNonSecretTemplateNames...,
		)

		// Add Disaster Recovery related templates, if the user wants disaster recovery.
		if config.ParsedGeneralConfig.Cloud.DisasterRecovery != nil {
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
		if config.UsingHCloud() {
			embeddedTemplateNames = append(embeddedTemplateNames,
				constants.HCloudCCMNonSecretTemplateNames...,
			)
		}
		if config.UsingHetznerBareMetal() {
			embeddedTemplateNames = append(embeddedTemplateNames,
				constants.HetznerCCMNonSecretTemplateNames...,
			)
		}

		if config.UsingHetznerBareMetal() {
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
			if config.RookCephEnabled() {
				embeddedTemplateNames = append(embeddedTemplateNames,
					constants.RookCephTemplateNames...,
				)
			}

			// When the control-plane is in Hetzner Bare Metal, and we're using a Failover IP,
			// we need the hetzner-robot ArgoCD App. It'll be responsible for switching the Failover IP
			// to a healthy master node, in a failover scenario.
			if config.ControlPlaneInHetznerBareMetal() &&
				config.ParsedGeneralConfig.Cloud.Hetzner.ControlPlane.BareMetal.Endpoint.IsFailoverIP {

				embeddedTemplateNames = append(embeddedTemplateNames,
					"argocd-apps/templates/hetzner-robot.yaml.tmpl",
					"argocd-apps/values-hetzner-robot.yaml.tmpl",
				)
			}
		}

		if config.UsingHCloud() {
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
	if config.VPNClusterEnabled() {
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
	if corenetbird.OperatorEnabled() {
		embeddedTemplateNames = append(embeddedTemplateNames,
			constants.NetBirdOperatorTemplateNames...,
		)
	}

	// hcloud-fip-controller — multi-CP HCloud VPN cluster only (a Coturn
	// Floating IP was provisioned). Keeps that Floating IP on the active
	// control-plane node so host-network Coturn survives CP failover.
	if config.CoturnFloatingIPEnabled() {
		embeddedTemplateNames = append(embeddedTemplateNames,
			constants.HCloudFIPControllerTemplateNames...,
		)
	}

	// Managed Keycloak only: kubeaid-cli installs the keycloakx
	// chart on this cluster and runs the gocloak realm reconciler
	// post-sync. External-mode VPN clusters skip this — the
	// operator's existing Keycloak handles it.
	if config.ManagedKeycloakEnabled() {
		embeddedTemplateNames = append(embeddedTemplateNames,
			constants.KeycloakManagedNonSecretTemplateNames...,
		)
	}

	// Obmondo customer: include the KubeAid Agent ArgoCD Application
	// templates when monitoring is requested.
	if config.ParsedGeneralConfig.Obmondo != nil && config.ParsedGeneralConfig.Obmondo.Monitoring {
		embeddedTemplateNames = append(embeddedTemplateNames,
			constants.KubeAidAgentNonSecretTemplateNames...,
		)
	}

	return embeddedTemplateNames
}

// acmeDNS01Enabled reports whether the cluster's ClusterIssuer uses a
// DNS-01 solver — the cluster.acmeDNS01 block is the single switch
// (parser validation guarantees acmeEmail + the provider token exist
// alongside it).
func acmeDNS01Enabled() bool {
	return config.ParsedGeneralConfig.Cluster.ACMEDNS01 != nil
}

// cloudflareAPIToken returns secrets.yaml's acme.cloudflareApiToken,
// nil-safe — the acme block only exists on clusters using DNS-01.
func cloudflareAPIToken() string {
	creds := config.ParsedSecretsConfig.ACME
	if creds == nil {
		return ""
	}
	return creds.CloudflareAPIToken
}

// printWorkloadOIDCBanner emits a one-time, operator-facing banner
// near the start of BootstrapCluster for workload-cluster runs:
//
//   - When cluster.keycloak is set: names the OIDC client the operator
//     must have created in their Keycloak realm
//     (kubernetes-<cluster.name>, public PKCE) and points at the doc
//     with the exact steps. Read-only check — kubeaid-cli does NOT
//     touch the operator's Keycloak admin API; the OIDC discovery
//     probe runs separately and validates realm reachability only.
//
//   - When cluster.keycloak is absent: warns that the cluster will
//     boot without OIDC and all users share admin.conf. Acceptable
//     for solo / dev clusters; bad practice for shared / production.
//
// No-op for VPN clusters — those run their own managed-Keycloak
// reconciler later in the bootstrap and don't need this banner.
func printWorkloadOIDCBanner(ctx context.Context) {
	cluster := config.ParsedGeneralConfig.Cluster
	if cluster.Type != constants.ClusterTypeWorkload {
		return
	}

	if cluster.Keycloak == nil {
		slog.WarnContext(ctx,
			"No cluster.keycloak block — bootstrap will continue without OIDC. "+
				"Operators authenticate via admin.conf, which is shared and bypasses "+
				"per-user RBAC. Not best practice for shared clusters. See "+
				"docs/workload-cluster-keycloak.md for the OIDC alternative.",
		)
		return
	}

	clientID := "kubernetes-" + cluster.Name
	slog.InfoContext(ctx,
		"Workload OIDC pre-flight",
		slog.String("realm_issuer", "https://"+cluster.Keycloak.DNS+"/auth/realms/"+cluster.Keycloak.Realm),
		slog.String("expected_client_id", clientID),
		slog.String("doc", "docs/workload-cluster-keycloak.md"),
	)
	slog.InfoContext(ctx,
		"Make sure a public PKCE OIDC client with the exact ID above exists "+
			"in that realm before running `kubeaid-cli login` — see the doc for "+
			"the create-client click-through.",
	)
}

// fetchNetBirdStatus is the test seam for requireOperatorOnNetBird —
// tests assign it before exercising the gate to avoid shelling out
// to a real `netbird` binary. Defaults to the real status fetcher.
var fetchNetBirdStatus = netbird.FetchStatus

// requireOperatorOnNetBird hard-fails the bootstrap when the
// operator's laptop isn't connected to the NetBird mesh AND the
// cluster about to be bootstrapped depends on mesh reachability
// (workload cluster + cluster.keycloak set — the Keycloak realm
// almost certainly lives on a private DNS reachable only through
// NetBird, and the OIDC discovery probe would fail later with a
// cryptic DNS error).
//
// Skipped when:
//   - cluster.type != workload (VPN clusters provision their own
//     Keycloak; the operator doesn't need the mesh to reach it
//     during bootstrap)
//   - cluster.keycloak is unset (no Keycloak to reach, the cluster
//     boots admin.conf-only — already covered by the workload OIDC
//     banner's WARN line)
//
// Returns an error suitable for assert.AssertErrNil — callers
// surface it to the bootstrap pipeline so the failure happens before
// any infrastructure is touched.
func requireOperatorOnNetBird(ctx context.Context) error {
	cluster := config.ParsedGeneralConfig.Cluster
	if cluster.Type != constants.ClusterTypeWorkload || cluster.Keycloak == nil {
		return nil
	}

	status, err := fetchNetBirdStatus(ctx)
	if err != nil {
		return fmt.Errorf(
			"querying NetBird daemon: %w — install netbird from "+
				"https://netbird.io and run `netbird up` against %s before "+
				"running `kubeaid-cli bootstrap` (the workload cluster's "+
				"Keycloak at %s is only reachable through the mesh)",
			err, cluster.Keycloak.DNS, cluster.Keycloak.DNS,
		)
	}

	if status.DaemonStatus != netbird.DaemonStatusConnected {
		return fmt.Errorf(
			"NetBird daemon status is %q, not %q — run `netbird up` to "+
				"connect to the mesh; the workload cluster's Keycloak at %s is "+
				"only reachable through NetBird",
			status.DaemonStatus, netbird.DaemonStatusConnected, cluster.Keycloak.DNS,
		)
	}

	// The daemon reports "Connected" — but to which mesh? An operator
	// who ran `netbird up` against a different NetBird server (their
	// personal netbird.io account, another customer's mesh) clears the
	// check above yet still cannot reach this cluster's Keycloak.
	//
	// Obmondo provisions Keycloak and NetBird Mgmt as siblings —
	// keycloak.<base> and netbird.<base> — so the mesh that hosts this
	// cluster's Keycloak is derivable from cluster.keycloak.dns. The
	// server check is skipped (not failed) when either side can't be
	// determined: a hard pre-flight gate must not block a valid
	// bootstrap on a guess.
	expectedHost := corenetbird.ExpectedHost(cluster.Keycloak.DNS)
	actualHost := status.Management.Host()

	switch {
	case expectedHost == "":
		slog.DebugContext(ctx,
			"skipping NetBird management-server check: cluster.keycloak.dns "+
				"is not a keycloak.<base> name, so the expected NetBird host "+
				"cannot be derived",
			slog.String("keycloakDNS", cluster.Keycloak.DNS),
		)

	case actualHost == "":
		slog.DebugContext(ctx,
			"skipping NetBird management-server check: the daemon reported "+
				"no parseable management URL",
			slog.String("managementURL", status.Management.URL),
		)

	case !strings.EqualFold(expectedHost, actualHost):
		return fmt.Errorf(
			"NetBird daemon is connected to the %q mesh, but this workload "+
				"cluster's Keycloak at %s lives on %q — run "+
				"`netbird up --management-url https://%s` to switch to the "+
				"correct mesh before running `kubeaid-cli bootstrap`",
			actualHost, cluster.Keycloak.DNS, expectedHost, expectedHost,
		)
	}

	return nil
}

// shouldValidateOIDCNow reports whether the pre-flight OIDC
// discovery probe should run at the start of bootstrap. True when
// apiServer.oidc is configured AND we aren't standing Keycloak up
// in this same bootstrap run (managed-Keycloak issuer doesn't
// exist yet — kubeaid-cli probes it via in-cluster port-forward
// later, after the keycloakx app syncs). Mirrors the internal
// skip in parser.ValidateOIDCDiscovery so the progress-bar spinner
// step is suppressed at the outer level too.
func shouldValidateOIDCNow() bool {
	cluster := config.ParsedGeneralConfig.Cluster
	if cluster.APIServer.OIDC == nil {
		return false
	}
	if cluster.Keycloak != nil && cluster.Keycloak.Mode == constants.KeycloakModeManaged {
		return false
	}
	return true
}

// hetznerBareMetalFirewallEnabled reports whether the Cilium host-firewall
// hostNetworkPolicy block should be rendered in the cilium values overlay.
// True when all of:
//   - cloud provider is Hetzner bare-metal (HetznerConfig.BareMetal non-nil)
//   - firewall.enabled is nil (default true) or explicitly true
//
// Nil-safe: returns false for any non-Hetzner-bare-metal configuration.
func hetznerBareMetalFirewallEnabled() bool {
	h := config.ParsedGeneralConfig.Cloud.Hetzner
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
// globals.ControlPlaneLBPrivateIP is populated). Without the IP, the
// hosts block would render empty and is useless.
func hcloudControlPlaneEndpointSet() bool {
	h := config.ParsedGeneralConfig.Cloud.Hetzner
	if h == nil || h.ControlPlane.HCloud == nil {
		return false
	}
	if h.ControlPlane.HCloud.LoadBalancer.Endpoint == "" {
		return false
	}
	return globals.ControlPlaneLBPrivateIP != ""
}

// hetznerControlPlaneLBPreProvisioned reports whether kubeaid-cli
// pre-creates the HCloud control-plane LB (so globals.ControlPlaneLB*
// are populated by template-render time). Mirrors the Hetzner-side
// gate in prerequisite_infrastructure.go's shouldPreCreateControlPlaneLB:
//
//	cluster.type=vpn        — this cluster IS the VPN (public LB).
//	HCloudVPNCluster set    — workload connecting to VPN (private LB).
//
// Used in getTemplateValues to decide whether to populate
// ControlPlaneEndpoint / ControlPlaneLB* from globals vs. fall through
// to the default (CAPI-managed) path.
func hetznerControlPlaneLBPreProvisioned() bool {
	cluster := config.ParsedGeneralConfig.Cluster
	h := config.ParsedGeneralConfig.Cloud.Hetzner
	if h == nil {
		return false
	}
	return cluster.Type == constants.ClusterTypeVPN || h.HCloudVPNCluster != nil
}

// Returns the list of embedded Secret template names based on the underlying cloud provider.
func getEmbeddedSecretTemplateNames() []string {
	// Templates common for all cloud providers.
	embeddedTemplateNames := constants.CommonSecretTemplateNames

	if config.ParsedGeneralConfig.Cluster.ArgoCD.DeployKeys.Kubeaid != nil {
		embeddedTemplateNames = append(embeddedTemplateNames,
			constants.KubeaidDeployKeySecretTemplateName,
		)
	}

	// Add cloud provider specific templates, if required.
	switch globals.CloudProviderName {
	case constants.CloudProviderAWS:
		embeddedTemplateNames = append(embeddedTemplateNames,
			constants.AWSSpecificSecretTemplateNames...,
		)

	case constants.CloudProviderAzure:
		embeddedTemplateNames = append(embeddedTemplateNames,
			constants.AzureSpecificSecretTemplateNames...,
		)

		// Add Disaster Recovery related templates, if the user wants disaster recovery.
		if config.ParsedGeneralConfig.Cloud.DisasterRecovery != nil {
			embeddedTemplateNames = append(embeddedTemplateNames,
				constants.AzureDisasterRecoverySpecificSecretTemplateNames...,
			)
		}

	case constants.CloudProviderHetzner:
		embeddedTemplateNames = append(embeddedTemplateNames,
			constants.CommonHetznerSpecificSecretTemplateNames...,
		)

		if config.UsingHetznerBareMetal() {
			embeddedTemplateNames = append(embeddedTemplateNames,
				constants.HetznerBareMetalSpecificSecretTemplateNames...,
			)
		}

	case constants.CloudProviderLocal:
		// No additional provider-specific secret templates needed.
	}

	if config.ParsedGeneralConfig.Obmondo != nil && config.ParsedGeneralConfig.Obmondo.Monitoring {
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
	if config.VPNClusterEnabled() {
		embeddedTemplateNames = append(embeddedTemplateNames,
			constants.NetBirdSecretTemplateNames...,
		)
	}

	// Managed Keycloak only: keycloak-admin SealedSecret.
	if config.ManagedKeycloakEnabled() {
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
	if corenetbird.OperatorEnabled() && corenetbird.APIKey() != "" {
		embeddedTemplateNames = append(embeddedTemplateNames,
			constants.NetBirdOperatorAPIKeySecretTemplateName,
		)
	}

	// hcloud-fip-controller token — the HCLOUD_API_TOKEN Secret the
	// controller reads via envFrom. Only when the controller is rendered.
	if config.CoturnFloatingIPEnabled() {
		embeddedTemplateNames = append(embeddedTemplateNames,
			constants.HCloudFIPControllerTokenSecretTemplateName,
		)
	}

	// Cloudflare API token for the DNS-01 ClusterIssuer — gated on the
	// cluster.acmeDNS01 block (parser validation already guarantees the
	// token exists alongside it).
	if acmeDNS01Enabled() {
		embeddedTemplateNames = append(embeddedTemplateNames,
			constants.CertManagerCloudflareAPITokenSecretTemplateName,
		)
	}

	return embeddedTemplateNames
}
