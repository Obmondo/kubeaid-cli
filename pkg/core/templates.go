package core

import (
	"context"
	"embed"
	"os"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud/aws"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/kubernetes"
	clusterAPIV1Beta1 "sigs.k8s.io/cluster-api/api/v1beta1"
)

//go:embed templates/*
var KubeaidConfigFileTemplates embed.FS

type TemplateValues struct {
	CustomerID,
	GitUsername,
	GitPassword string
	config.ClusterConfig
	config.ForksConfig
	AWSConfig     *config.AWSConfig
	HetznerConfig *config.HetznerConfig
	config.MonitoringConfig
	CAPIClusterNamespace,
	AWSB64EncodedCredentials,
	AWSAccountID string
	ProvisionedClusterEndpoint *clusterAPIV1Beta1.APIEndpoint
}

func getTemplateValues() *TemplateValues {
	templateValues := &TemplateValues{
		CustomerID:           config.ParsedConfig.CustomerID,
		GitUsername:          config.ParsedConfig.Git.Username,
		GitPassword:          config.ParsedConfig.Git.Password,
		ClusterConfig:        config.ParsedConfig.Cluster,
		ForksConfig:          config.ParsedConfig.Forks,
		AWSConfig:            config.ParsedConfig.Cloud.AWS,
		HetznerConfig:        config.ParsedConfig.Cloud.Hetzner,
		MonitoringConfig:     config.ParsedConfig.Monitoring,
		CAPIClusterNamespace: kubernetes.GetCapiClusterNamespace(),
	}

	// Set cloud provider specific values.
	switch globals.CloudProviderName {
	case constants.CloudProviderAWS:
		templateValues.AWSAccountID = aws.GetAccountID(context.Background())
		templateValues.AWSB64EncodedCredentials = os.Getenv(constants.EnvNameAWSB64EcodedCredentials)
	}

	// Set control-plane endpoint, if cluster has been provisioned. The control-plane endpoint will
	// be used to create the Cilium ArgoCD App.
	//
	// NOTE :
	//
	//  (1) This executes before we do `clusterctl move`.
	//
	//  (2) Initially, Cilium is installed in kube-proxyless mode in the provisioned cluster, using
	//      the postKubeadm hook in the KubeadmControlPlane resource. After the cluster has been
	//      provisioned, we bring it in the GitOPs cycle.
	{
		ctx := context.Background()

		managementClusterClient, _ := kubernetes.CreateKubernetesClient(ctx, constants.OutputPathManagementClusterContainerKubeconfig, true)

		if cluster, err := kubernetes.GetClusterResource(ctx, managementClusterClient); err == nil {
			templateValues.ProvisionedClusterEndpoint = &cluster.Spec.ControlPlaneEndpoint
		}
	}

	return templateValues
}

// Returns the list of embedded (non Secret) template names based on the underlying cloud provider.
func getEmbeddedNonSecretTemplateNames() []string {
	// Templates common for all cloud providers.
	embeddedTemplateNames := append(constants.CommonNonSecretTemplateNames, constants.CommonCloudNonSecretTemplateNames...)

	// Add cloud provider specific templates.
	switch globals.CloudProviderName {
	case constants.CloudProviderAWS:
		embeddedTemplateNames = append(embeddedTemplateNames, constants.AWSSpecificNonSecretTemplateNames...)
		//
		// Add Disaster Recovery related templates, if disasterRecovery section is specified in the
		// cloud-provider specific config.
		if config.ParsedConfig.Cloud.AWS.DisasterRecovery != nil {
			embeddedTemplateNames = append(embeddedTemplateNames, constants.AWSDisasterRecoverySpecificTemplateNames...)
		}

	case constants.CloudProviderHetzner:
		embeddedTemplateNames = append(embeddedTemplateNames, constants.HetznerSpecificNonSecretTemplateNames...)

	case constants.CloudProviderLocal:
		embeddedTemplateNames = constants.CommonNonSecretTemplateNames
	}

	// Add Obmondo K8s Agent related templates, if 'monitoring.connectObmondo' is set to true.
	if config.ParsedConfig.Monitoring.ConnectObmondo {
		embeddedTemplateNames = append(embeddedTemplateNames,
			"argocd-apps/templates/obmondo-k8s-agent.app.yaml.tmpl",
			"argocd-apps/obmondo-k8s-agent.values.yaml.tmpl",
		)
	}

	return embeddedTemplateNames
}

// Returns the list of embedded Secret template names based on the underlying cloud provider.
func getEmbeddedSecretTemplateNames() []string {
	// Templates common for all cloud providers.
	embeddedTemplateNames := append(constants.CommonSecretTemplateNames, constants.CommonCloudSecretTemplateNames...)

	// Add cloud provider specific templates, if required.
	switch globals.CloudProviderName {
	case constants.CloudProviderHetzner:
		embeddedTemplateNames = append(embeddedTemplateNames, constants.HetznerSpecificSecretTemplateNames...)
	case constants.CloudProviderLocal:
		embeddedTemplateNames = constants.CommonSecretTemplateNames
	}

	return embeddedTemplateNames
}
