package constants

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

	FlagNameConfig = "config"

	FlagNameSkipKubePrometheusBuild = "skip-kube-prometheus-build"
	FlagNameSkipClusterctlMove      = "skip-clusterctl-move"

	FlagNameDeleteOldCluster = "delete-old-cluster"

	FlagNameHetznerAPIToken      = "hetzner-cloud-api-token"
	FlagNameHetznerRobotUsername = "hetzner-robot-username"
	FlagNameHetznerRobotPassword = "hetzner-robot-password"

	FlagNameAWSAccessKey    = "aws-access-key-id"
	FlagNameAWSSecretKey    = "aws-secret-access-key"
	FlagNameAWSSessionToken = "aws-session-token"
	FlagNameAWSRegion       = "aws-region"
	FlagNameAMIID           = "ami-id"
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
)

// Output paths.
const (
	OutputPathGeneratedConfig = "./outputs/kubeaid-bootstrap-script.config.yaml"

	OutputPathManagementClusterHostKubeconfig      = "./outputs/management-cluster.host.kubeconfig.yaml"
	OutputPathManagementClusterContainerKubeconfig = "./outputs/management-cluster.container.kubeconfig.yaml"

	OutputPathProvisionedClusterKubeconfig = "./outputs/provisioned-cluster.kubeconfig.yaml"
)

// ArgoCD App names.
const (
	ArgoCDAppRoot              = "root"
	ArgoCDAppCapiCluster       = "capi-cluster"
	ArgoCDAppHetznerRobot      = "hetzner-robot"
	ArgoCDAppClusterAutoscaler = "cluster-autoscaler"
	ArgoCDAppVelero            = "velero"
	ArgoCDAppKubePrometheus    = "kube-prometheus"
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

	CommonNonSecretTemplateNames = []string{
		// For Cilium
		"argocd-apps/templates/cilium.app.yaml.tmpl",
		"argocd-apps/cilium.values.yaml.tmpl",

		// For ArgoCD.
		"argocd-apps/templates/argo-cd.app.yaml.tmpl",
		"argocd-apps/argo-cd.values.yaml.tmpl",

		// For Root ArgoCD App.
		"argocd-apps/Chart.yaml",
		"argocd-apps/templates/root.yaml.tmpl",

		// For KubePrometheus.
		"argocd-apps/templates/kube-prometheus.app.yaml.tmpl",

		// For CertManager.
		"argocd-apps/templates/cert-manager.app.yaml.tmpl",
		"argocd-apps/cert-manager.values.yaml.tmpl",

		// For Sealed Secrets.
		"argocd-apps/templates/sealed-secrets.app.yaml.tmpl",
		"argocd-apps/sealed-secrets.values.yaml.tmpl",
		"argocd-apps/templates/secrets.app.yaml.tmpl",

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

	TemplateNameJsonnet = "cluster-vars.jsonnet.tmpl"
)
