package constants

import (
	"github.com/Obmondo/kubeaid-bootstrap-script/config"
)

const (
	FlagNameK8sVersion         = "k8s-version"
	FlagNameCloud              = "cloud"
	FlagNameConfigFile         = "config-file"
	FlagNameSkipClusterctlMove = "skip-clusterctl-move"

	CloudProviderAWS     = "aws"
	CloudProviderAzure   = "azure"
	CloudProviderHetzner = "hetzner"

	EnvNameAWSB64EcodedCredentials = "AWS_B64ENCODED_CREDENTIALS"

	TemplateNameAWSSampleConfig   = "aws.sample.config.yaml.tmpl"
	TemplateNameJsonnet           = "cluster-vars.jsonnet.tmpl"
	TemplateNameKubeaidConfigRepo = "sealed-secrets/argo-cd/kubeaid-config.yaml.tmpl"

	OutputPathManagementClusterKubeconfig  = "./outputs/management-cluster.kubeconfig.yaml"
	OutputPathProvisionedClusterKubeconfig = "./outputs/provisioned-cluster.kubeconfig.yaml"
	OutputPathGeneratedConfig              = "./outputs/kubeaid-bootstrap-script.config.yaml"

	// Supported Kubernetes versions.
	K8s_v1_30_0 = "v1.30.0"
	K8s_v1_31_0 = "v1.31.0"
)

var (
	SupportedK8sVersions = []string{K8s_v1_31_0, K8s_v1_30_0}

	// Custom ARM and Ubuntu based AMI for each supported Kubernetes version, built by and published
	// from Obmondo's AWS account.
	ObmondoPublishedAMIs = map[string]string{
		K8s_v1_31_0: "ami-0fc044fa0d061d18d",
		K8s_v1_30_0: "ami-0c4219b11327ef260",
	}

	TempDir string

	ParsedConfig *config.Config

	CommonEmbeddedTemplateNames = []string{
		// ArgoCD.
		"argocd-apps/templates/argo-cd.app.yaml.tmpl",
		"argocd-apps/argo-cd.values.yaml.tmpl",

		// Root ArgoCD App.
		"argocd-apps/Chart.yaml",
		"argocd-apps/templates/root.yaml.tmpl",

		// KubePrometheus.
		"argocd-apps/templates/kube-prometheus.app.yaml.tmpl",

		// CertManager.
		"argocd-apps/templates/cert-manager.app.yaml.tmpl",
		"argocd-apps/cert-manager.values.yaml.tmpl",

		// Sealed Secrets.
		"argocd-apps/templates/sealed-secrets.app.yaml.tmpl",
		"argocd-apps/sealed-secrets.values.yaml.tmpl",
		"argocd-apps/templates/secrets.app.yaml.tmpl",
		TemplateNameKubeaidConfigRepo,

		// Cluster API.
		"argocd-apps/templates/cluster-api.app.yaml.tmpl",
		"argocd-apps/cluster-api.values.yaml.tmpl",

		// Traefik.
		"argocd-apps/templates/traefik.app.yaml.tmpl",
		"argocd-apps/traefik.values.yaml.tmpl",
	}

	AWSSpecificEmbeddedTemplateNames = []string{
		// CAPI Cluster.
		"argocd-apps/templates/capi-cluster.app.yaml.tmpl",
		"argocd-apps/capi-cluster.values.yaml.tmpl",

		// AWS Cloud Controller Manager.
		"argocd-apps/templates/ccm-aws.yaml.tmpl",
		"argocd-apps/ccm-aws.values.yaml.tmpl",
	}
)
