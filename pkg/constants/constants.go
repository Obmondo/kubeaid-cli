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
	FlagNameManagementClusterNameDefaultValue = "kubeaid-bootstrapper"

	FlagNameConfigsDirectoy = "configs-directory"

	FlagNameSkipMonitoringSetup = "skip-monitoring-setup"
	FlagNameSkipPRWorkflow      = "skip-pr-workflow"
	FlagNameSkipClusterctlMove  = "skip-clusterctl-move"

	FlagNameAWSAccessKeyID     = "aws-access-key-id"
	FlagNameAWSSecretAccessKey = "aws-secret-access-key"
	FlagNameAWSSessionToken    = "aws-session-token"
	FlagNameAWSRegion          = "aws-region"
	FlagNameAMIID              = "ami-id"

	FlagNameAzureClientSecret = "azure-client-secret"
	FlagNameImageID           = "image-id"

	FlagNameHetznerAPIToken      = "hetzner-cloud-api-token"
	FlagNameHetznerRobotUsername = "hetzner-robot-username"
	FlagNameHetznerRobotPassword = "hetzner-robot-password"
)

// Kube API server CLI flags.
const (
	KubeAPIServerFlagAuditPolicyFile = "audit-policy-file"
	KubeAPIServerFlagAuditLogPath    = "audit-log-path"
)

// Cloud providers.
const (
	CloudProviderAWS       = "aws"
	CloudProviderHetzner   = "hetzner"
	CloudProviderAzure     = "azure"
	CloudProviderBareMetal = "bare-metal"
	CloudProviderLocal     = "local"
)

// Output paths.
var (
	OutputDirectory = "./outputs"

	OutputPathGeneratedConfigsDirectory  = path.Join(OutputDirectory, "configs/")
	OutputPathGeneratedGeneralConfigFile = path.Join(
		OutputPathGeneratedConfigsDirectory,
		FileNameGeneralConfig,
	)
	OutputPathGeneratedSecretsConfigFile = path.Join(
		OutputPathGeneratedConfigsDirectory,
		FileNameSecretsConfig,
	)

	OutputPathLogFile = path.Join(OutputDirectory, ".log")

	OutputPathManagementClusterK3DConfig = path.Join(OutputDirectory, "k3d.config.yaml")

	OutputPathManagementClusterHostKubeconfig = path.Join(
		OutputDirectory,
		"kubeconfigs/clusters/management/host.yaml",
	)
	OutputPathManagementClusterContainerKubeconfig = path.Join(
		OutputDirectory,
		"kubeconfigs/clusters/management/container.yaml",
	)

	OutputPathMainClusterKubeconfig = path.Join(OutputDirectory, "kubeconfigs/clusters/main.yaml")

	OutputPathJWKSDocument = path.Join(
		OutputDirectory,
		"workload-identity/openid-provider/jwks.json",
	)
)

// ArgoCD.
const (
	ReleaseNameArgoCD = "argocd"

	ArgoCDProjectKubeAid = "kubeaid"

	// Apps.
	ArgoCDAppRoot              = "root"
	ArgoCDAppCapiCluster       = "capi-cluster"
	ArgoCDAppHetznerRobot      = "hetzner-robot"
	ArgoCDAppClusterAutoscaler = "cluster-autoscaler"
	ArgoCDAppVelero            = "velero"
	ArgoCDAppKubePrometheus    = "kube-prometheus"
	ArgoCDExternalSnapshotter  = "external-snapshotter"
)

// Sealed Secrets.
const (
	SealedSecretsControllerName    = "sealed-secrets-controller"
	CRONJobNameBackupSealedSecrets = "backup-sealed-secrets"
)

// Azure
const (
	BlobContainerNameOIDCProvider = "oidc-provider"

	AzureBlobNameOpenIDConfiguration = ".well-known/openid-configuration"
	AzureBlobNameJWKSDocument        = "openid/v1/jwks"

	// NOTE : You can view all the Azure built-in roles here :
	//        https://learn.microsoft.com/en-us/azure/role-based-access-control/built-in-roles.

	// Grants full access to manage all resources, but does not allow you to assign roles in Azure
	// RBAC, manage assignments in Azure Blueprints, or share image galleries.
	AzureRoleIDContributor = "b24988ac-6180-42a0-ab88-20f7382dd24c"
	// Provides full access to Azure Storage blob containers and data, including assigning POSIX
	// access control.
	AzureRoleIDStorageBlobDataOwner = "b7e6dc6d-f1e8-4753-8033-0f276bb0955b"

	AzureResponseStatusCodeResourceAlreadyExists = 409

	UAMIClusterAPI            = "cluster-api"
	UAMIVelero                = "velero"
	UAMISealedSecretsBackuper = "sealed-secrets-backuper"
)

// Hetzner
const (
	HetznerModeBareMetal = "bare-metal"
	HetznerModeHCloud    = "hcloud"
	HetznerModeHybrid    = "hybrid"

	HetznerRobotWebServiceAPI = "https://robot-ws.your-server.de"
)

const (
	// Namespaces.
	NamespaceArgoCD        = "argocd"
	NamespaceVelero        = "velero"
	NamespaceSealedSecrets = "sealed-secrets"
	NamespaceCilium        = "cilium"
	NamespaceCiliumTest    = "cilium-test"

	// Service Accounts.
	ServiceAccountCAPZ          = "capz-manager"
	ServiceAccountASO           = "azureserviceoperator-default"
	ServiceAccountVelero        = "velero"
	ServiceAccountSealedSecrets = "sealed-secrets"
)

// File names
const (
	FileNameGeneralConfig = "general.yaml"
	FileNameSecretsConfig = "secrets.yaml"
)

// SSH keys.
const (
	SSHPrivateKeyPrefixOpenSSH = "-----BEGIN OPENSSH PRIVATE KEY-----"
	SSHPublicKeyPrefixOpenSSH  = "ssh-rsa "

	SSHPrivateKeyPrefixPEM = "-----BEGIN PRIVATE KEY-----"
	SSHPublicKeyPrefixPEM  = "-----BEGIN PUBLIC KEY-----"
)

// Cluster types.
const (
	ClusterTypeManagement = "management"
	ClusterTypeMain       = "main"
)

// Miscellaneous.
const (
	RepoURLObmondoKubeAid = "https://github.com/Obmondo/KubeAid"

	GzippedFilenameSuffix = ".gz"
)
