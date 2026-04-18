// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package core

import (
	"context"
	"crypto/x509"
	"embed"
	"encoding/pem"
	"log/slog"
	"os"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud/aws"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud/azure"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
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

		We specify that pre-provisioned LB IP as the control-plane endpoint to CAPI and Cilium.

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

	// Subject CN of the Obmondo-issued mTLS cert (ObmondoConfig.CertPath),
	// populated when Obmondo.Monitoring is true. Used in
	// cluster-vars.jsonnet.tmpl as the required `certname` field.
	ObmondoCertCN string
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

		HetznerConfig:      config.ParsedGeneralConfig.Cloud.Hetzner,
		HetznerCredentials: config.ParsedSecretsConfig.Hetzner,

		BareMetalConfig: config.ParsedGeneralConfig.Cloud.BareMetal,

		DisasterRecoveryConfig: config.ParsedGeneralConfig.Cloud.DisasterRecovery,

		ObmondoConfig: config.ParsedGeneralConfig.Obmondo,

		ExtraKnownHosts: config.ParsedGeneralConfig.Git.KnownHosts,
	}

	// Extract the Subject CN from the Obmondo mTLS cert when monitoring is on.
	// kube-prometheus's common-template fails hard if certname is missing.
	if config.ParsedGeneralConfig.Obmondo != nil && config.ParsedGeneralConfig.Obmondo.Monitoring {
		templateValues.ObmondoCertCN = readCertCN(ctx,
			config.ParsedGeneralConfig.Obmondo.CertPath)
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

	hetznerConfig := config.ParsedGeneralConfig.Cloud.Hetzner

	// Set the control-plane endpoint.
	switch globals.CloudProviderName {
	case constants.CloudProviderHetzner:
		switch {
		// Hetzner Bare Metal cluster; the user specifies it.
		case hetznerConfig.Mode == constants.HetznerModeBareMetal:
			templateValues.ControlPlaneEndpoint = hetznerConfig.ControlPlane.BareMetal.Endpoint.Host

		// HCloud / Hetzner hybrid cluster, when we have a VPN cluster.
		// We've pre-provisioned the control-plane LB.
		case ((hetznerConfig.Mode == constants.HetznerModeHCloud) || (hetznerConfig.Mode == constants.HetznerModeHybrid) && (hetznerConfig.HCloudVPNCluster != nil)):
			templateValues.ControlPlaneEndpoint = globals.PreProvisionedControlPlaneLBIP
		}

	// Bare Metal cluster; the user specifies it.
	case constants.CloudProviderBareMetal:
		templateValues.ControlPlaneEndpoint = config.ParsedGeneralConfig.Cloud.BareMetal.ControlPlane.Endpoint.Host

	default:
		// For local/dev clusters, the main cluster endpoint may not be available yet.
		if endpoint := kubernetes.GetMainClusterEndpoint(ctx); endpoint != nil {
			templateValues.ControlPlaneEndpoint = endpoint.Hostname()
		}
	}

	return templateValues
}

// readCertCN reads a PEM-encoded X.509 certificate and returns its Subject
// Common Name. Fatal if the file can't be read or parsed.
func readCertCN(ctx context.Context, path string) string {
	data, err := os.ReadFile(path)
	assert.AssertErrNil(ctx, err,
		"Failed reading Obmondo cert file", slog.String("path", path))

	block, _ := pem.Decode(data)
	assert.Assert(ctx, block != nil,
		"Failed PEM-decoding Obmondo cert file (no PEM block found)",
		slog.String("path", path))

	cert, err := x509.ParseCertificate(block.Bytes)
	assert.AssertErrNil(ctx, err,
		"Failed parsing Obmondo cert", slog.String("path", path))

	return cert.Subject.CommonName
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

	// Obmondo customer: include the KubeAid Agent (and sibling teleport-kube-agent)
	// ArgoCD Application templates when monitoring is requested.
	if config.ParsedGeneralConfig.Obmondo != nil && config.ParsedGeneralConfig.Obmondo.Monitoring {
		embeddedTemplateNames = append(embeddedTemplateNames,
			constants.CustomerSpecificNonSecretTemplateNames...,
		)
	}

	return embeddedTemplateNames
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

	//nolint:staticcheck,revive
	if config.ParsedGeneralConfig.Obmondo != nil {
		//nolint:godox
		// TODO : Some regex validation, and add customer specific templates

		//nolint:staticcheck,revive
		if config.ParsedGeneralConfig.Obmondo.Monitoring {
			//nolint:godox
			// TODO : Enable monitoring for the customer and setup kubeaid-agent
		}
	}

	return embeddedTemplateNames
}
