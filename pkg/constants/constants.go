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

	FlagNameConfig = "config"

	FlagNameSkipKubePrometheusBuild = "skip-kube-prometheus-build"
	FlagNameSkipClusterctlMove      = "skip-clusterctl-move"

	FlagNameDeleteOldCluster = "delete-old-cluster"

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

	OutputPathGeneratedConfig = path.Join(OutputDirectory, "kubeaid-bootstrap-script.config.yaml")

	OutputPathManagementClusterK3DConfig           = path.Join(OutputDirectory, "management-cluster.config.yaml")
	OutputPathManagementClusterHostKubeconfig      = path.Join(OutputDirectory, "management-cluster.host.kubeconfig.yaml")
	OutputPathManagementClusterContainerKubeconfig = path.Join(OutputDirectory, "management-cluster.container.kubeconfig.yaml")

	OutputPathProvisionedClusterKubeconfig = path.Join(OutputDirectory, "provisioned-cluster.kubeconfig.yaml")

	OutputPathJWKSDocument = path.Join(OutputDirectory, "jwks.json")
)

// ArgoCD.
const (
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

// Uncategorized.
const (
	RepoURLObmondoKubeAid = "https://github.com/Obmondo/KubeAid"

	NamespaceArgoCD = "argo-cd"
)

// Template names.
var (
	TemplateNameAWSSampleConfig     = "files/templates/aws.sample.config.yaml.tmpl"
	TemplateNameHetznerSampleConfig = "files/templates/hetzner.sample.config.yaml.tmpl"
	TemplateNameLocalSampleConfig   = "files/templates/local.sample.config.yaml.tmpl"

	TemplateNameOpenIDConfig = "templates/openid-configuration.json.tmpl"

	TemplateNameK3DConfig = "templates/k3d.config.yaml.tmpl"

	CommonNonSecretTemplateNames = []string{
		// For ArgoCD.
		"argocd-apps/templates/argo-cd.app.yaml.tmpl",
		"argocd-apps/argo-cd.values.yaml.tmpl",

		// For Root ArgoCD App.
		"argocd-apps/Chart.yaml",
		"argocd-apps/templates/root.yaml.tmpl",

		// For CertManager.
		"argocd-apps/templates/cert-manager.app.yaml.tmpl",
		"argocd-apps/cert-manager.values.yaml.tmpl",

		// For Sealed Secrets.
		"argocd-apps/templates/sealed-secrets.app.yaml.tmpl",
		"argocd-apps/sealed-secrets.values.yaml.tmpl",
		"argocd-apps/templates/secrets.app.yaml.tmpl",
	}

	// For KubePrometheus.
	TemplateNameKubePrometheusArgoCDApp = "argocd-apps/templates/kube-prometheus.app.yaml.tmpl"
	TemplateNameKubePrometheusVars      = "cluster-vars.jsonnet.tmpl"

	CommonCloudNonSecretTemplateNames = []string{
		// For Cilium
		"argocd-apps/templates/cilium.app.yaml.tmpl",
		"argocd-apps/cilium.values.yaml.tmpl",

		// For Cluster API.
		"argocd-apps/templates/cluster-api.app.yaml.tmpl",
		"argocd-apps/cluster-api.values.yaml.tmpl",

		// For CAPI Cluster.
		"argocd-apps/templates/capi-cluster.app.yaml.tmpl",
		"argocd-apps/capi-cluster.values.yaml.tmpl",

		// For Cluster Autoscaler.
		"argocd-apps/templates/cluster-autoscaler.app.yaml.tmpl",
		"argocd-apps/cluster-autoscaler.values.yaml.tmpl",
	}

	CommonSecretTemplateNames = []string{
		// For ArgoCD.
		"sealed-secrets/argo-cd/kubeaid-config.yaml.tmpl",
	}

	CommonCloudSecretTemplateNames = []string{
		// For Cluster API.
		"sealed-secrets/capi-cluster/cloud-credentials.yaml.tmpl",
	}

	AWSSpecificNonSecretTemplateNames = []string{
		// For AWS Cloud Controller Manager.
		"argocd-apps/templates/ccm-aws.app.yaml.tmpl",
		"argocd-apps/ccm-aws.values.yaml.tmpl",
	}

	AWSDisasterRecoverySpecificTemplateNames = []string{
		// For Kube2IAM.
		"argocd-apps/templates/kube2iam.app.yaml.tmpl",
		"argocd-apps/kube2iam.values.yaml.tmpl",

		// For Velero.
		"argocd-apps/templates/velero.app.yaml.tmpl",
		"argocd-apps/velero.values.yaml.tmpl",

		// For K8sConfigs.
		"argocd-apps/templates/k8s-configs.app.yaml.tmpl",
		"k8s-configs/sealed-secrets.namespace.yaml.tmpl",
		"k8s-configs/velero.namespace.yaml.tmpl",
	}

	AzureSpecificNonSecretTemplateNames = []string{
		// For Azure Cloud Controller Manager.
		"argocd-apps/templates/ccm-azure.app.yaml.tmpl",
		"argocd-apps/ccm-azure.values.yaml.tmpl",
	}

	HetznerSpecificNonSecretTemplateNames = []string{
		// For Hetzner Robot Failover.
		// "argocd-apps/templates/hetzner-robot.app.yaml.tmpl",
		// "argocd-apps/hetzner-robot.values.yaml.tmpl",

		// For Hetzner Cloud Controller Manager.
		"argocd-apps/templates/ccm-hetzner.app.yaml.tmpl",
		"argocd-apps/ccm-hetzner.values.yaml.tmpl",
	}

	HetznerSpecificSecretTemplateNames = []string{
		// For Cluster API.
		// "sealed-secrets/capi-cluster/hetzner-robot-ssh-keypair.yaml.tmpl",
		"sealed-secrets/kube-system/cloud-credentials.yaml.tmpl",
	}
)
