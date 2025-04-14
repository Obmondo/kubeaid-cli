package constants

import "path"

// Environment variable names.
const (
	EnvNameAWSAccessKey            = "AWS_ACCESS_KEY_ID"
	EnvNameAWSSecretKey            = "AWS_SECRET_ACCESS_KEY"
	EnvNameAWSSessionToken         = "AWS_SESSION_TOKEN"
	EnvNameAWSRegion               = "AWS_REGION"
	EnvNameAWSB64EcodedCredentials = "AWS_B64ENCODED_CREDENTIALS"

	EnvNameHCloudToken   = "HCLOUD_TOKEN"
	EnvNameRobotUser     = "ROBOT_USER"
	EnvNameRobotPassword = "ROBOT_PASSWORD"

	EnvNameKubeconfig = "KUBECONFIG"
)

// CLI flags.
const (
	FlagNameDebug = "debug"

	FlagNameK8sVersion = "k8s-version"

	FlagNameManagementClusterName             = "management-cluster-name"
	FlagNameManagementClusterNameDefaultValue = "management-cluster"

	FlagNameConfigsDirectoy = "configs-directory"

	FlagNameSkipMonitoringSetup     = "skip-monitoring-setup"
	FlagNameSkipKubePrometheusBuild = "skip-kube-prometheus-build"
	FlagNameSkipPRFlow              = "skip-pr-flow"
	FlagNameSkipClusterctlMove      = "skip-clusterctl-move"

	FlagNameAWSAccessKeyID     = "aws-access-key-id"
	FlagNameAWSSecretAccessKey = "aws-secret-access-key"
	FlagNameAWSSessionToken    = "aws-session-token"
	FlagNameAWSRegion          = "aws-region"
	FlagNameAMIID              = "ami-id"

	FlagNameHetznerAPIToken      = "hetzner-cloud-api-token"
	FlagNameHetznerRobotUsername = "hetzner-robot-username"
	FlagNameHetznerRobotPassword = "hetzner-robot-password"

	FlagNameAzureClientSecret = "azure-client-secret"
)

// Kube API server CLI flags.
const (
	KubeAPIServerFlagAuditPolicyFile = "audit-policy-file"
	KubeAPIServerFlagAuditLogPath    = "audit-log-path"
)

// Cloud providers.
const (
	CloudProviderAWS     = "aws"
	CloudProviderHetzner = "hetzner"
	CloudProviderAzure   = "azure"
	CloudProviderLocal   = "local"
)

// Output paths.
var (
	OutputDirectory = "./outputs"

	OutputPathGeneratedConfigsDirectory = path.Join(OutputDirectory, "configs/")
	FileNameGeneralConfig               = "general.yaml"
	FileNameSecretsConfig               = "secrets.yaml"

	OutputPathLogFile = path.Join(OutputDirectory, ".log")

	OutputPathManagementClusterK3DConfig = path.Join(OutputDirectory, "k3d.config.yaml")

	OutputPathManagementClusterHostKubeconfig      = path.Join(OutputDirectory, "kubeconfigs/clusters/management/host.yaml")
	OutputPathManagementClusterContainerKubeconfig = path.Join(OutputDirectory, "kubeconfigs/clusters/management/container.yaml")

	OutputPathMainClusterKubeconfig = path.Join(OutputDirectory, "kubeconfigs/clusters/main.yaml")

	OutputPathJWKSDocument = path.Join(OutputDirectory, "workload-identity/openid-provider/jwks.json")
)

// ArgoCD.
const (
	NamespaceArgoCD   = "argocd"
	ReleaseNameArgoCD = "argocd"

	// Project.
	ArgoCDProjectKubeAid = "kubeaid"

	// Apps.
	ArgoCDAppRoot              = "root"
	ArgoCDAppCapiCluster       = "capi-cluster"
	ArgoCDAppHetznerRobot      = "hetzner-robot"
	ArgoCDAppClusterAutoscaler = "cluster-autoscaler"
	ArgoCDAppVelero            = "velero"
	ArgoCDAppKubePrometheus    = "kube-prometheus"
)

// Azure
const (
	BlobContainerNameWorkloadIdentity = "workload-identity-oidc-provider"

	AzureBlobNameOpenIDConfiguration = ".well-known/openid-configuration"
	AzureBlobNameJWKSDocument        = "openid/v1/jwks"

	AzureRoleIDContributor    = "b24988ac-6180-42a0-ab88-20f7382dd24c"
	AzureStorageBlobDataOwner = "b7e6dc6d-f1e8-4753-8033-0f276bb0955b"

	AzureResponseStatusCodeResourceAlreadyExists = 409

	ServiceAccountNameCAPZ = "capz-manager"
	ServiceAccountNameASO  = "azureserviceoperator-default"
)

// Miscellaneous.
const (
	RepoURLObmondoKubeAid = "https://github.com/Obmondo/KubeAid"

	ClusterTypeManagement = "management"
	ClusterTypeMain       = "main"
)

// Template names.
var (
	TemplateNameAWSGeneralConfig = "files/templates/aws.general.config.yaml.tmpl"
	TemplateNameAWSSecretsConfig = "files/templates/aws.secrets.config.yaml.tmpl"

	TemplateNameAzureGeneralConfig = "files/templates/azure.general.config.yaml.tmpl"
	TemplateNameAzureSecretsConfig = "files/templates/azure.secrets.config.yaml.tmpl"

	TemplateNameHetznerGeneralConfig = "files/templates/hetzner.general.config.yaml.tmpl"
	TemplateNameHetznerSecretsConfig = "files/templates/hetzner.secrets.config.yaml.tmpl"

	TemplateNameLocalGeneralConfig = "files/templates/local.general.config.yaml.tmpl"
	TemplateNameLocalSecretsConfig = "files/templates/local.secrets.config.yaml.tmpl"

	TemplateNameOpenIDConfig = "templates/openid-configuration.json.tmpl"

	TemplateNameK3DConfig = "templates/k3d.config.yaml.tmpl"

	CommonNonSecretTemplateNames = []string{
		// For ArgoCD.
		"argocd-apps/templates/argocd.yaml.tmpl",
		"argocd-apps/values-argocd.yaml.tmpl",

		// For Root ArgoCD App.
		"argocd-apps/Chart.yaml",
		"argocd-apps/templates/root.yaml.tmpl",

		// For CertManager.
		"argocd-apps/templates/cert-manager.yaml.tmpl",
		"argocd-apps/values-cert-manager.yaml.tmpl",

		// For Sealed Secrets.
		"argocd-apps/templates/sealed-secrets.yaml.tmpl",
		"argocd-apps/values-sealed-secrets.yaml.tmpl",
		"argocd-apps/templates/secrets.yaml.tmpl",
	}

	CommonSecretTemplateNames = []string{
		// For ArgoCD.
		"sealed-secrets/argocd/kubeaid-config.yaml.tmpl",
	}

	// For KubePrometheus.
	TemplateNameKubePrometheusArgoCDApp = "argocd-apps/templates/kube-prometheus.yaml.tmpl"
	TemplateNameKubePrometheusVars      = "cluster-vars.jsonnet.tmpl"

	CommonCloudNonSecretTemplateNames = []string{
		// For Cilium
		"argocd-apps/templates/cilium.yaml.tmpl",
		"argocd-apps/values-cilium.yaml.tmpl",

		// For Cluster API.
		"argocd-apps/templates/cluster-api.yaml.tmpl",
		"argocd-apps/values-cluster-api.yaml.tmpl",

		// For CAPI Cluster.
		"argocd-apps/templates/capi-cluster.yaml.tmpl",
		"argocd-apps/values-capi-cluster.yaml.tmpl",

		// For Cluster Autoscaler.
		"argocd-apps/templates/cluster-autoscaler.yaml.tmpl",
		"argocd-apps/values-cluster-autoscaler.yaml.tmpl",
	}

	AWSSpecificNonSecretTemplateNames = []string{
		// For AWS Cloud Controller Manager.
		"argocd-apps/templates/ccm-aws.yaml.tmpl",
		"argocd-apps/values-ccm-aws.yaml.tmpl",
	}

	AWSSpecificSecretTemplateNames = []string{
		// For Cluster API.
		"sealed-secrets/capi-cluster/cloud-credentials.yaml.tmpl",
	}

	AWSDisasterRecoverySpecificTemplateNames = []string{
		// For Kube2IAM.
		"argocd-apps/templates/kube2iam.yaml.tmpl",
		"argocd-apps/values-kube2iam.yaml.tmpl",

		// For Velero.
		"argocd-apps/templates/velero.yaml.tmpl",
		"argocd-apps/values-velero.yaml.tmpl",

		// For K8sConfigs.
		"argocd-apps/templates/k8s-configs.yaml.tmpl",
		"k8s-configs/sealed-secrets.namespace.yaml.tmpl",
		"k8s-configs/velero.namespace.yaml.tmpl",
	}

	AzureSpecificNonSecretTemplateNames = []string{
		// For Azure Cloud Controller Manager.
		"argocd-apps/templates/ccm-azure.yaml.tmpl",
		"argocd-apps/values-ccm-azure.yaml.tmpl",
	}

	HetznerSpecificNonSecretTemplateNames = []string{
		// For Hetzner Robot Failover.
		// "argocd-apps/templates/hetzner-robot.yaml.tmpl",
		// "argocd-apps/values-hetzner-robot.yaml.tmpl",

		// For Hetzner Cloud Controller Manager.
		"argocd-apps/templates/ccm-hetzner.yaml.tmpl",
		"argocd-apps/values-ccm-hetzner.yaml.tmpl",
	}

	HetznerSpecificSecretTemplateNames = []string{
		// For Cluster API.
		// "sealed-secrets/capi-cluster/hetzner-robot-ssh-keypair.yaml.tmpl",
		"sealed-secrets/capi-cluster/cloud-credentials.yaml.tmpl",
		"sealed-secrets/kube-system/cloud-credentials.yaml.tmpl",
	}
)
