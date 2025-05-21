package constants

// Common template names.
var (
	CommonNonSecretTemplateNames = []string{
		// For KubeAid Bootstrap Script general config.
		"kubeaid-bootstrap-script.general.yaml.tmpl",

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
)

// Common template names (for clusters being provisioned in any of the supported cloud providers).
var (
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

		// For External Snapshotter.
		"argocd-apps/templates/external-snapshotter.yaml.tmpl",
	}
)

// AWS specific template names.
var (
	AWSSpecificNonSecretTemplateNames = []string{
		// For AWS Cloud Controller Manager.
		"argocd-apps/templates/ccm-aws.yaml.tmpl",
		"argocd-apps/values-ccm-aws.yaml.tmpl",
	}

	AWSSpecificSecretTemplateNames = []string{
		// For Cluster API.
		"sealed-secrets/capi-cluster/cloud-credentials.yaml.tmpl",
	}

	AWSDisasterRecoverySpecificNonSecretTemplateNames = []string{
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
)

// Azure specific template names.
var (
	TemplateNameOpenIDConfig = "templates/openid-configuration.json.tmpl"

	AzureSpecificNonSecretTemplateNames = []string{
		// For Azure Cloud Controller Manager.
		"argocd-apps/templates/ccm-azure.yaml.tmpl",
		"argocd-apps/values-ccm-azure.yaml.tmpl",

		// For Azure Disk CSI Driver.
		"argocd-apps/templates/azuredisk-csi-driver.yaml.tmpl",
		"argocd-apps/values-azuredisk-csi-driver.yaml.tmpl",

		// For Azure Workload Identity System Webhook.
		"argocd-apps/templates/azure-workload-identity-webhook.yaml.tmpl",
		"argocd-apps/values-azure-workload-identity-webhook.yaml.tmpl",
	}

	AzureSpecificSecretTemplateNames = []string{
		"sealed-secrets/capi-cluster/service-account-issuer-keys.yaml.tmpl",
	}

	AzureDisasterRecoverySpecificNonSecretTemplateNames = []string{
		// For Velero.
		"argocd-apps/templates/velero.yaml.tmpl",
		"argocd-apps/values-velero.yaml.tmpl",
	}

	AzureDisasterRecoverySpecificSecretTemplateNames = []string{
		// For Sealed Secrets Backuper.
		"sealed-secrets/sealed-secrets/backup-sealed-secrets-pod-env.yaml.tmpl",
	}
)

// Hetzner specific template names.
var (
	HCloudSpecificNonSecretTemplateNames = []string{
		// For Hetzner Cloud Controller Manager.
		"argocd-apps/templates/ccm-hetzner.yaml.tmpl",
		"argocd-apps/values-ccm-hetzner.yaml.tmpl",

		// For HCloud CSI driver.
		"argocd-apps/templates/hcloud-csi-driver.yaml.tmpl",
		"argocd-apps/values-hcloud-csi-driver.yaml.tmpl",
	}

	HCloudSpecificSecretTemplateNames = []string{
		// For Hetzner Cloud Controller Manager.
		"sealed-secrets/kube-system/cloud-credentials.yaml.tmpl",

		// For Cluster API.
		"sealed-secrets/capi-cluster/cloud-credentials.yaml.tmpl",
	}
)

// Config template names.
var (
	TemplateNameAWSGeneralConfig = "files/templates/aws.general.config.yaml.tmpl"
	TemplateNameAWSSecretsConfig = "files/templates/aws.secrets.config.yaml.tmpl"

	TemplateNameAzureGeneralConfig = "files/templates/azure.general.config.yaml.tmpl"
	TemplateNameAzureSecretsConfig = "files/templates/azure.secrets.config.yaml.tmpl"

	TemplateNameHetznerGeneralConfig = "files/templates/hetzner.general.config.yaml.tmpl"
	TemplateNameHetznerSecretsConfig = "files/templates/hetzner.secrets.config.yaml.tmpl"

	TemplateNameHCloudGeneralConfig = "files/templates/hcloud.general.config.yaml.tmpl"
	TemplateNameHCloudSecretsConfig = "files/templates/hcloud.secrets.config.yaml.tmpl"

	TemplateNameLocalGeneralConfig = "files/templates/local.general.config.yaml.tmpl"
	TemplateNameLocalSecretsConfig = "files/templates/local.secrets.config.yaml.tmpl"
)

// Miscallaneous.
const (
	TemplateNameK3DConfig = "templates/k3d.config.yaml.tmpl"

	// For KubePrometheus.
	TemplateNameKubePrometheusArgoCDApp = "argocd-apps/templates/kube-prometheus.yaml.tmpl"
	TemplateNameKubePrometheusVars      = "cluster-vars.jsonnet.tmpl"
)
