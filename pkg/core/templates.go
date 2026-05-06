// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package core

import (
	"context"
	"embed"
	"log/slog"
	"os"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud/aws"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud/azure"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/keycloak"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/kubernetes"
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

	// ControlPlaneExtraCertSANs are extra DNS names rendered into
	// the chart's values so kubeadm includes them in the apiserver
	// TLS cert SAN list (alongside the primary endpoint). Used for
	// mesh-side hostnames like a NetBird-form name.
	ControlPlaneExtraCertSANs []string

	ExtraKnownHosts []string

	*config.DisasterRecoveryConfig

	*config.ObmondoConfig
	*config.ObmondoCredentials

	// Subject CN of the Obmondo-issued mTLS cert (ObmondoConfig.CertPath),
	// populated when Obmondo.Monitoring is true. Used in
	// cluster-vars.jsonnet.tmpl as the required `certname` field.
	ObmondoCertCN string

	// Raw file contents of ObmondoConfig.CertPath / KeyPath, populated when
	// Obmondo.Monitoring is true. Base64-encoded into the obmondo-clientcert
	// sealed-secret templates (one per consuming namespace). Stored as strings
	// because go-sprout's base64Encode takes a string, not []byte.
	ObmondoCertFileContents string
	ObmondoKeyFileContents  string

	// KeycloakAdminPassword is the plaintext password templated into
	// the keycloak-admin SealedSecret. Populated only when
	// managedKeycloakEnabled.
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
}

func getTemplateValues(ctx context.Context) *TemplateValues {
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

		HetznerConfig:      sanitizedHetznerConfigForChart(config.ParsedGeneralConfig.Cloud.Hetzner),
		HetznerCredentials: config.ParsedSecretsConfig.Hetzner,

		BareMetalConfig: config.ParsedGeneralConfig.Cloud.BareMetal,

		DisasterRecoveryConfig: config.ParsedGeneralConfig.Cloud.DisasterRecovery,

		ObmondoConfig:      config.ParsedGeneralConfig.Obmondo,
		ObmondoCredentials: config.ParsedSecretsConfig.Obmondo,

		ExtraKnownHosts: config.ParsedGeneralConfig.Git.KnownHosts,
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

	if vpnClusterEnabled() {
		// Cluster client is nil pre-bootstrap; the read-or-generate
		// helpers fall back to fresh values in that case and the
		// first SealedSecret sync establishes the stable values.
		// Re-runs read the persisted values so we never drift from
		// what's already deployed.
		clusterClient, _ := kubernetes.CreateKubernetesClient(ctx,
			constants.OutputPathMainClusterKubeconfig,
		)

		datastoreKey, err := keycloak.GetOrGenerateBase64Key(ctx, clusterClient,
			constants.NamespaceNetBird,
			constants.SecretNameNetBird,
			constants.SecretKeyNetBirdDatastoreKey,
			constants.NetBirdDatastoreKeyByteLen,
		)
		assert.AssertErrNil(ctx, err, "Failed reading/generating NetBird datastore encryption key")
		templateValues.NetBirdDatastoreKey = datastoreKey

		relayPwd, err := keycloak.GetOrGenerateClientSecret(ctx, clusterClient,
			constants.NamespaceNetBird,
			constants.SecretNameNetBird,
			constants.SecretKeyNetBirdRelayPassword,
		)
		assert.AssertErrNil(ctx, err, "Failed reading/generating NetBird relay password")
		templateValues.NetBirdRelayPassword = relayPwd

		turnPwd, err := keycloak.GetOrGenerateClientSecret(ctx, clusterClient,
			constants.NamespaceNetBird,
			constants.SecretNameNetBird,
			constants.SecretKeyNetBirdTurnPassword,
		)
		assert.AssertErrNil(ctx, err, "Failed reading/generating NetBird TURN password")
		templateValues.NetBirdTurnPassword = turnPwd

		// postgresDSN is CNPG-generated and only available in-cluster
		// after the netbird-pgsql Cluster CR is reconciled. On the
		// first kubeaid-cli run the Secret doesn't have the key yet
		// → render an empty string; bootstrap_cluster.go's
		// patchNetBirdPostgresDSN step fills it in post-sync. On
		// re-runs the patched value is read back here so the
		// SealedSecret in git stays in sync.
		templateValues.NetBirdPostgresDSN = readSecretValueOrEmpty(ctx, clusterClient,
			constants.NamespaceNetBird,
			constants.SecretNameNetBird,
			constants.SecretKeyNetBirdPostgresDSN,
		)

		templateValues.NetBirdClientID = constants.NetBirdClientID
		templateValues.NetBirdBackendClientID = constants.NetBirdBackendClientID

		// netbird-backend OIDC client secret: source depends on mode.
		//   managed:  read-or-generate; same value passed to
		//             ReconcileClient so Keycloak stores what NetBird
		//             expects to envFrom.
		//   external: operator-supplied via secrets.yaml; we have no
		//             way to mint or look up the value otherwise. The
		//             validator (added separately) ensures the field
		//             is present before bootstrap proceeds.
		if managedKeycloakEnabled() {
			nbSecret, err := keycloak.GetOrGenerateClientSecret(ctx, clusterClient,
				constants.NamespaceNetBird,
				constants.SecretNameNetBird,
				constants.SecretKeyNetBirdIDPMgmtSecret,
			)
			assert.AssertErrNil(ctx, err, "Failed reading/generating NetBird backend client secret")
			templateValues.NetBirdBackendClientSecret = nbSecret
		} else if config.ParsedSecretsConfig.Keycloak != nil {
			templateValues.NetBirdBackendClientSecret = config.ParsedSecretsConfig.Keycloak.NetBirdBackendClientSecret
		}
	}

	if managedKeycloakEnabled() {
		clusterClient, _ := kubernetes.CreateKubernetesClient(ctx,
			constants.OutputPathMainClusterKubeconfig,
		)

		adminPwd, err := keycloak.GetOrGenerateClientSecret(ctx, clusterClient,
			constants.NamespaceKeycloak,
			constants.SecretNameKeycloakAdmin,
			constants.SecretKeyKeycloakPassword,
		)
		assert.AssertErrNil(ctx, err, "Failed reading/generating Keycloak admin password")
		templateValues.KeycloakAdminPassword = adminPwd
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

		// HCloud / Hetzner hybrid cluster with a VPN cluster. The
		// control-plane LB is pre-provisioned; endpoint is the
		// hostname when configured (clients resolve via CoreDNS to
		// the LB private IP), else the private IP directly.
		case (((hetznerConfig.Mode == constants.HetznerModeHCloud) || (hetznerConfig.Mode == constants.HetznerModeHybrid)) && (hetznerConfig.HCloudVPNCluster != nil)):
			templateValues.ControlPlaneLBPrivateIP = globals.ControlPlaneLBPrivateIP
			templateValues.ControlPlaneLBBootstrapPublicIP = globals.ControlPlaneLBBootstrapPublicIP
			templateValues.ControlPlaneExtraCertSANs = hetznerConfig.ControlPlane.HCloud.LoadBalancer.ExtraCertSANs
			if globals.ControlPlaneHostname != "" {
				templateValues.ControlPlaneEndpoint = globals.ControlPlaneHostname
				break
			}
			templateValues.ControlPlaneEndpoint = globals.ControlPlaneLBPrivateIP
		}

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
		if config.UsingHetznerBareMetal() {
			embeddedTemplateNames = append(embeddedTemplateNames,
				constants.HetznerBareMetalSpecificNonSecretTemplateNames...,
			)

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
	// Ingress (and Keycloak ingress when managed) and CloudNativePG
	// for NetBird's Postgres backend. cnpg also backs keycloak-pgsql
	// in managed mode; rendering it here keeps cnpg syncing once
	// regardless of mode.
	if vpnClusterEnabled() {
		embeddedTemplateNames = append(embeddedTemplateNames,
			constants.TraefikTemplateNames...,
		)
		embeddedTemplateNames = append(embeddedTemplateNames,
			constants.CloudNativePGTemplateNames...,
		)
	}

	// Managed Keycloak only: kubeaid-cli installs the keycloakx
	// chart on this cluster and runs the gocloak realm reconciler
	// post-sync. External-mode VPN clusters skip this — the
	// operator's existing Keycloak handles it.
	if managedKeycloakEnabled() {
		embeddedTemplateNames = append(embeddedTemplateNames,
			constants.KeycloakManagedNonSecretTemplateNames...,
		)
	}

	// Obmondo customer: include the KubeAid Agent (and optionally
	// teleport-kube-agent) ArgoCD Application templates when monitoring is
	// requested. Teleport defaults on; operators can set
	// obmondo.teleportAgent: false to skip it (e.g. test envs without a join
	// token, or clusters waiting on the Netbird-backed gateway replacement).
	if config.ParsedGeneralConfig.Obmondo != nil && config.ParsedGeneralConfig.Obmondo.Monitoring {
		embeddedTemplateNames = append(embeddedTemplateNames,
			constants.KubeAidAgentNonSecretTemplateNames...,
		)

		if teleportAgentEnabled() {
			embeddedTemplateNames = append(embeddedTemplateNames,
				constants.TeleportKubeAgentNonSecretTemplateNames...,
			)
		}
	}

	return embeddedTemplateNames
}

// managedKeycloakEnabled reports whether kubeaid-cli should
// install Keycloak itself on this cluster. True only when
// cluster.type=vpn AND cluster.keycloak.mode=managed.
//
// Gates the kubeaid-cli-side Keycloak install: the keycloakx
// ArgoCD App, the keycloak-admin SealedSecret, and the post-sync
// gocloak realm reconciler. Does NOT gate cnpg, traefik, the
// netbird Secret, or the postgres DSN patch — those are needed
// in both modes; see vpnClusterEnabled() for that.
//
// Workload clusters always return false (they don't host Keycloak).
// Nil-safe.
func managedKeycloakEnabled() bool {
	cluster := config.ParsedGeneralConfig.Cluster
	if cluster.Type != constants.ClusterTypeVPN || cluster.Keycloak == nil {
		return false
	}
	return cluster.Keycloak.Mode == constants.KeycloakModeManaged
}

// vpnClusterEnabled reports whether kubeaid-cli should render the
// VPN-cluster-wide infrastructure: cnpg (for NetBird's Postgres),
// traefik (for NetBird's Ingress), the netbird /
// netbird-turn-credentials SealedSecrets, and the post-sync
// postgres DSN patch.
//
// True for any VPN cluster regardless of Keycloak mode — external
// Keycloak still needs the same surrounding stack because NetBird
// itself runs in-cluster, only the OIDC IdP is offsite. Workload
// clusters always return false.
//
// Equivalent in practice to cluster.type=vpn with a keycloak block
// present (validator requires the block for VPN clusters), but
// expressed as its own function so callers don't have to reason
// about the validator's invariants.
func vpnClusterEnabled() bool {
	cluster := config.ParsedGeneralConfig.Cluster
	return cluster.Type == constants.ClusterTypeVPN && cluster.Keycloak != nil
}

// hcloudControlPlaneEndpointSet reports whether kubeaid-cli should
// render the cluster-side coredns-custom ConfigMap for resolving
// the apiserver endpoint. True only when the operator configured
// loadBalancer.endpoint on an HCloud control-plane.
func hcloudControlPlaneEndpointSet() bool {
	h := config.ParsedGeneralConfig.Cloud.Hetzner
	if h == nil || h.ControlPlane.HCloud == nil {
		return false
	}
	return h.ControlPlane.HCloud.LoadBalancer.Endpoint != ""
}

// teleportAgentEnabled reports whether the teleport-kube-agent ArgoCD App
// should be rendered. Only meaningful when obmondo.monitoring is true; nil
// (unset) counts as enabled so existing configs keep their current behavior.
// Nil-safe — returns false when Obmondo isn't configured at all.
func teleportAgentEnabled() bool {
	obmondo := config.ParsedGeneralConfig.Obmondo
	if obmondo == nil {
		return false
	}
	return obmondo.TeleportAgent == nil || *obmondo.TeleportAgent
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
		// mTLS client cert: consumed by kubeaid-agent (Obmondo API auth) and
		// Alertmanager (alert push). Always required when monitoring is on —
		// CertPath/KeyPath validated in validateConfigs.
		embeddedTemplateNames = append(embeddedTemplateNames,
			constants.ObmondoClientCertSecretTemplateNames...,
		)

		// alertmanager-main: Alertmanager's runtime config Secret.
		embeddedTemplateNames = append(embeddedTemplateNames,
			constants.AlertmanagerMainSecretTemplateName,
		)

		// teleport-kube-agent join-token. Paired with
		// TeleportKubeAgentNonSecretTemplateNames — toggled together.
		if teleportAgentEnabled() {
			embeddedTemplateNames = append(embeddedTemplateNames,
				constants.TeleportKubeAgentSecretTemplateNames...,
			)
		}
	}

	// VPN cluster (any Keycloak mode): netbird + netbird-turn-credentials
	// SealedSecrets always. The OIDC client secret inside the
	// netbird Secret is generated by kubeaid-cli when managed,
	// supplied by the operator via secrets.yaml when external.
	if vpnClusterEnabled() {
		embeddedTemplateNames = append(embeddedTemplateNames,
			constants.NetBirdSecretTemplateNames...,
		)
	}

	// Managed Keycloak only: keycloak-admin SealedSecret.
	if managedKeycloakEnabled() {
		embeddedTemplateNames = append(embeddedTemplateNames,
			constants.KeycloakManagedSecretTemplateNames...,
		)
	}

	return embeddedTemplateNames
}
