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
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/kubernetes"
)

//go:embed templates/*
var KubeaidConfigFileTemplates embed.FS

type TemplateValues struct {
	CustomerID,
	GitUsername,
	GitPassword string
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

	HetznerConfig *config.HetznerConfig

	ProvisionedClusterEndpoint *clusterAPIV1Beta1.APIEndpoint
}

func getTemplateValues(ctx context.Context) *TemplateValues {
	templateValues := &TemplateValues{
		CustomerID:             config.ParsedGeneralConfig.CustomerID,
		GitUsername:            config.ParsedSecretsConfig.Git.Username,
		GitPassword:            config.ParsedSecretsConfig.Git.Password,
		ForksConfig:            config.ParsedGeneralConfig.Forks,
		ClusterConfig:          config.ParsedGeneralConfig.Cluster,
		DisasterRecoveryConfig: config.ParsedGeneralConfig.Cloud.DisasterRecovery,
		MonitoringConfig:       config.ParsedGeneralConfig.Monitoring,
		CAPIClusterNamespace:   kubernetes.GetCapiClusterNamespace(),

		AWSConfig:     config.ParsedGeneralConfig.Cloud.AWS,
		AzureConfig:   config.ParsedGeneralConfig.Cloud.Azure,
		HetznerConfig: config.ParsedGeneralConfig.Cloud.Hetzner,
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
	{
		ctx := context.Background()

		kubeConfigPaths := []string{
			kubernetes.GetManagementClusterKubeconfigPath(ctx),
			constants.OutputPathMainClusterKubeconfig,
		}

		for _, kubeConfigPath := range kubeConfigPaths {
			clusterClient, err := kubernetes.CreateKubernetesClient(ctx, kubeConfigPath)
			if err != nil {
				continue
			}

			cluster, err := kubernetes.GetClusterResource(ctx, clusterClient)
			if err == nil {
				templateValues.ProvisionedClusterEndpoint = &cluster.Spec.ControlPlaneEndpoint
				break
			}
		}
	}

	return templateValues
}

// Returns the list of embedded (non Secret) template names based on the underlying cloud provider.
func getEmbeddedNonSecretTemplateNames() []string {
	// Templates common for all cloud providers.
	embeddedTemplateNames := append(constants.CommonNonSecretTemplateNames,
		constants.CommonCloudNonSecretTemplateNames...,
	)

	// Add cloud provider specific templates.
	switch globals.CloudProviderName {
	case constants.CloudProviderAWS:
		embeddedTemplateNames = append(embeddedTemplateNames,
			constants.AWSSpecificNonSecretTemplateNames...,
		)

		// Add Disaster Recovery related templates, if the user wants disaster recover.
		if config.ParsedGeneralConfig.Cloud.DisasterRecovery != nil {
			embeddedTemplateNames = append(embeddedTemplateNames,
				constants.AWSDisasterRecoverySpecificNonSecretTemplateNames...,
			)
		}

	case constants.CloudProviderAzure:
		embeddedTemplateNames = append(embeddedTemplateNames,
			constants.AzureSpecificNonSecretTemplateNames...,
		)

		// Add Disaster Recovery related templates, if the user wants disaster recover.
		if config.ParsedGeneralConfig.Cloud.DisasterRecovery != nil {
			embeddedTemplateNames = append(embeddedTemplateNames,
				constants.AzureDisasterRecoverySpecificNonSecretTemplateNames...,
			)
		}

	case constants.CloudProviderHetzner:
		embeddedTemplateNames = append(embeddedTemplateNames,
			constants.HetznerSpecificNonSecretTemplateNames...,
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
			constants.HetznerSpecificSecretTemplateNames...,
		)

	case constants.CloudProviderLocal:
		embeddedTemplateNames = constants.CommonSecretTemplateNames
	}

	return embeddedTemplateNames
}
