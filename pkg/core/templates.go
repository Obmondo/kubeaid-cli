package core

import (
	"context"
	"embed"
	"os"

	clusterAPIV1Beta1 "sigs.k8s.io/cluster-api/api/v1beta1"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud/aws"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud/azure"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/git"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/kubernetes"
)

//go:embed templates/*
var KubeaidConfigFileTemplates embed.FS

type TemplateValues struct {
	GeneralConfigFileContents string

	CustomerID,
	CustomerGitServerHostname string
	config.GitConfig
	config.GitCredentials
	config.ForksConfig
	config.ClusterConfig
	*config.DisasterRecoveryConfig
	config.MonitoringConfig
	CAPIClusterNamespace string

	AWSConfig *config.AWSConfig
	AWSB64EncodedCredentials,
	AWSAccountID string

	AzureConfig *config.AzureConfig
	ServiceAccountIssuerURL,
	UAMIClientIDClusterAPI,
	UAMIClientIDVelero,
	AzureStorageAccountAccessKey string

	HetznerConfig      *config.HetznerConfig
	HetznerCredentials *config.HetznerCredentials

	BareMetalConfig *config.BareMetalConfig

	ProvisionedClusterEndpoint *clusterAPIV1Beta1.APIEndpoint
}

func getTemplateValues(ctx context.Context) *TemplateValues {
	templateValues := &TemplateValues{
		GeneralConfigFileContents: string(config.GeneralConfigFileContents),

		CustomerID:                config.ParsedGeneralConfig.CustomerID,
		CustomerGitServerHostname: git.GetCustomerGitServerHostName(ctx),
		GitConfig:                 config.ParsedGeneralConfig.Git,
		GitCredentials:            config.ParsedSecretsConfig.Git,
		ForksConfig:               config.ParsedGeneralConfig.Forks,
		ClusterConfig:             config.ParsedGeneralConfig.Cluster,
		DisasterRecoveryConfig:    config.ParsedGeneralConfig.Cloud.DisasterRecovery,
		MonitoringConfig:          config.ParsedGeneralConfig.Monitoring,
		CAPIClusterNamespace:      kubernetes.GetCapiClusterNamespace(),

		AWSConfig: config.ParsedGeneralConfig.Cloud.AWS,

		AzureConfig: config.ParsedGeneralConfig.Cloud.Azure,

		HetznerConfig:      config.ParsedGeneralConfig.Cloud.Hetzner,
		HetznerCredentials: config.ParsedSecretsConfig.Hetzner,

		BareMetalConfig: config.ParsedGeneralConfig.Cloud.BareMetal,
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
		templateValues.UAMIClientIDClusterAPI = globals.UAMIClientIDClusterAPI
		templateValues.UAMIClientIDVelero = globals.UAMIClientIDVelero
		templateValues.AzureStorageAccountAccessKey = globals.AzureStorageAccountAccessKey
	}

	/*
		Set control-plane endpoint, if cluster has been provisioned (detects by trying to query the
		Cluster resource from the management / provisioned cluster).
		The control-plane endpoint will be used to create the Cilium ArgoCD App.

		NOTE : Initially, Cilium is installed in kube-proxyless mode in the provisioned cluster, using
		the postKubeadm hook in the KubeadmControlPlane resource. After the cluster has been
		provisioned, we bring it in the GitOPs cycle.
	*/
	templateValues.ProvisionedClusterEndpoint = kubernetes.GetMainClusterEndpoint(ctx)

	return templateValues
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
		if config.IsUsingHetznerBareMetal() {
			embeddedTemplateNames = append(embeddedTemplateNames,
				constants.HetznerBareMetalSpecificNonSecretTemplateNames...,
			)

			// When the control-plane is in Hetzner Bare Metal, and we're using a Failover IP,
			// we need the hetzner-robot ArgoCD App. It'll be responsible for switching the Failover IP
			// to a healthy master node, in a failover scenario.
			if config.IsControlPlaneInHetznerBareMetal() &&
				config.ParsedGeneralConfig.Cloud.Hetzner.ControlPlane.BareMetal.Endpoint.IsFailoverIP {

				embeddedTemplateNames = append(embeddedTemplateNames,
					"argocd-apps/templates/hetzner-robot.yaml.tmpl",
					"argocd-apps/values-hetzner-robot.yaml.tmpl",
				)
			}
		}

		if config.IsUsingHCloud() {
			embeddedTemplateNames = append(embeddedTemplateNames,
				constants.HCloudSpecificNonSecretTemplateNames...,
			)
		}

	case constants.CloudProviderBareMetal:
		embeddedTemplateNames = append(constants.CommonNonSecretTemplateNames,
			"kubeone/kubeone-cluster.yaml.tmpl",
		)

	case constants.CloudProviderLocal:
		embeddedTemplateNames = constants.CommonNonSecretTemplateNames
	}

	// Add Obmondo K8s Agent related templates, if 'monitoring.connectObmondo' is set to true.
	if config.ParsedGeneralConfig.Monitoring.ConnectObmondo {
		embeddedTemplateNames = append(embeddedTemplateNames,
			"argocd-apps/templates/obmondo-k8s-agent.yaml.tmpl",
			"argocd-apps/values-obmondo-k8s-agent.yaml.tmpl",
		)
	}

	return embeddedTemplateNames
}

// Returns the list of embedded Secret template names based on the underlying cloud provider.
func getEmbeddedSecretTemplateNames() []string {
	// Templates common for all cloud providers.
	embeddedTemplateNames := constants.CommonSecretTemplateNames

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

		if config.IsUsingHetznerBareMetal() {
			embeddedTemplateNames = append(embeddedTemplateNames,
				constants.HetznerBareMetalSpecificSecretTemplateNames...,
			)
		}

	case constants.CloudProviderLocal:
		embeddedTemplateNames = constants.CommonSecretTemplateNames
	}

	return embeddedTemplateNames
}
