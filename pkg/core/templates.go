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
	// same value is templated into the netbird-keycloak
	// SealedSecret AND passed through to ReconcileClient as
	// spec.Secret so Keycloak stores what NetBird's chart already
	// expects in the cluster Secret — single git push, single
	// sync.
	NetBirdBackendClientSecret string
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

	if managedKeycloakEnabled() {
		// Both secrets are read-or-generate: regenerating on a
		// re-run would drift the in-cluster Secret from the value
		// Keycloak still holds (the chart's pre-install hook bakes
		// the admin password once; gocloak ignores spec.Secret on
		// existing clients). Cluster client is nil pre-bootstrap,
		// in which case both calls fall back to a fresh value and
		// the first SealedSecret sync establishes the stable one.
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

		nbSecret, err := keycloak.GetOrGenerateClientSecret(ctx, clusterClient,
			constants.NamespaceNetBird,
			constants.SecretNameNetBirdKeycloak,
			constants.SecretKeyNetBirdSecret,
		)
		assert.AssertErrNil(ctx, err, "Failed reading/generating NetBird backend client secret")
		templateValues.NetBirdBackendClientSecret = nbSecret
	}

	// Set cloud provider specific values.
	switch globals.CloudProviderName {
	case constants.CloudProviderAWS:
		templateValues.AWSAccountID = aws.GetAccountID(ctx)
		templateValues.AWSB64EncodedCredentials = os.Getenv(
			constants.EnvNameAWSB64EcodedCredentials,
		)

	case constants.CloudProviderAzure:
		templateValues.ServiceAccountIssuerURL = azure.GetServiceAccountIssuerURL(ctx)
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
		hcloudControlPlane.LoadBalancer.Hostname = ""
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

	// Managed Keycloak: when this is the VPN cluster and cluster.keycloak.mode
	// is "managed", render the keycloakx + cloudnative-pg ArgoCD apps so
	// Keycloak comes up backed by CNPG Postgres on first sync. Realm / client
	// reconciliation happens later (kubeaid-cli + gocloak via port-forward to
	// the keycloakx Service).
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

// managedKeycloakEnabled reports whether kubeaid-cli should render
// the keycloakx + cloudnative-pg ArgoCD Apps for THIS cluster. True
// only when cluster.type=vpn AND cluster.keycloak.mode=managed —
// only VPN clusters host Keycloak, and only in managed mode does
// kubeaid-cli install it (mode=external means an existing Keycloak
// elsewhere). Workload clusters always return false: they don't host
// Keycloak; they authenticate kube-apiserver against an existing one
// via apiServer.oidc (issuer URL + client ID, set in their own
// general.yaml). Nil-safe — Cluster.Keycloak is absent on workload
// clusters and on VPN clusters that opt out.
func managedKeycloakEnabled() bool {
	cluster := config.ParsedGeneralConfig.Cluster
	if cluster.Type != constants.ClusterTypeVPN || cluster.Keycloak == nil {
		return false
	}
	return cluster.Keycloak.Mode == "managed"
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

	if managedKeycloakEnabled() {
		embeddedTemplateNames = append(embeddedTemplateNames,
			constants.KeycloakManagedSecretTemplateNames...,
		)
	}

	return embeddedTemplateNames
}
