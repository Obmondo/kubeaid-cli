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

	return templateValues
}

// Returns the list of embedded (non Secret) template names based on the underlying cloud provider.
func getEmbeddedNonSecretTemplateNames() []string {
	// Templates common for all cloud providers.
	embeddedTemplateNames := constants.CommonNonSecretTemplateNames

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
	embeddedTemplateNames := constants.CommonSecretTemplateNames

	// Add cloud provider specific templates, if required.
	switch globals.CloudProviderName {
	case constants.CloudProviderHetzner:
		embeddedTemplateNames = append(embeddedTemplateNames, constants.HetznerSpecificSecretTemplateNames...)
	}

	return embeddedTemplateNames
}
