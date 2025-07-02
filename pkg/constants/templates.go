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
	CommonCloudSpecificNonSecretTemplateNames = []string{
		// For Cilium
		"argocd-apps/templates/cilium.yaml.tmpl",
		"argocd-apps/values-cilium.yaml.tmpl",

		// For Cluster API.
		"argocd-apps/templates/cluster-api-operator.yaml.tmpl",
		"argocd-apps/values-cluster-api-operator.yaml.tmpl",
		"argocd-apps/templates/capi-cluster.yaml.tmpl",
		"argocd-apps/values-capi-cluster.yaml.tmpl",
	}
)

// AWS specific template names.
var (
	AWSSpecificNonSecretTemplateNames = []string{
		// For AWS Cloud Controller Manager.
		"argocd-apps/templates/ccm-aws.yaml.tmpl",
		"argocd-apps/values-ccm-aws.yaml.tmpl",

		// For Cluster Autoscaler.
		"argocd-apps/templates/cluster-autoscaler.yaml.tmpl",
		"argocd-apps/values-cluster-autoscaler.yaml.tmpl",

		// For External Snapshotter.
		"argocd-apps/templates/external-snapshotter.yaml.tmpl",
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

		// For Cluster Autoscaler.
		"argocd-apps/templates/cluster-autoscaler.yaml.tmpl",
		"argocd-apps/values-cluster-autoscaler.yaml.tmpl",

		// For External Snapshotter.
		"argocd-apps/templates/external-snapshotter.yaml.tmpl",
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
	CommonHetznerSpecificSecretTemplateNames = []string{
		// For HCloud Cloud Controller Manager.
		"sealed-secrets/kube-system/cloud-credentials.yaml.tmpl",

		// For Cluster API.
		"sealed-secrets/capi-cluster/cloud-credentials.yaml.tmpl",
	}

	HCloudSpecificNonSecretTemplateNames = []string{
		// For HCloud Cloud Controller Manager.
		"argocd-apps/templates/ccm-hcloud.yaml.tmpl",
		"argocd-apps/values-ccm-hcloud.yaml.tmpl",

		// For HCloud CSI driver.
		"argocd-apps/templates/hcloud-csi-driver.yaml.tmpl",
		"argocd-apps/values-hcloud-csi-driver.yaml.tmpl",

		// For Cluster Autoscaler.
		"argocd-apps/templates/cluster-autoscaler.yaml.tmpl",
		"argocd-apps/values-cluster-autoscaler.yaml.tmpl",
	}

	HetznerBareMetalSpecificNonSecretTemplateNames = []string{
		// For Hetzner Bare Metal (Syself's) Cloud Controller Manager.
		"argocd-apps/templates/ccm-hetzner.yaml.tmpl",
		"argocd-apps/values-ccm-hetzner.yaml.tmpl",

		// For LocalPV provisioner.
		"argocd-apps/templates/localpv-provisioner.yaml.tmpl",
		"argocd-apps/values-localpv-provisioner.yaml.tmpl",
	}

	HetznerBareMetalSpecificSecretTemplateNames = []string{
		// For Cluster API.
		"sealed-secrets/capi-cluster/hetzner-ssh-keypair.yaml.tmpl",
	}
)

// Bare metal specific template names.
var BareMetalSpecificNonSecretTemplateNames = []string{
	// For LocalPV provisioner.
	"argocd-apps/templates/localpv-provisioner.yaml.tmpl",
	"argocd-apps/values-localpv-provisioner.yaml.tmpl",
}

// Obmondo customer specific template names.
var (
	CustomerSpecificNonSecretTemplateNames = []string{
		// For Teleport Kube Agent component.
		// NOTE : When we'll have support for provisioning gateway cluster running Netbird,
		//        Teleport will be removed.
		"argocd-apps/templates/teleport-kube-agent.yaml.tmpl",
		"argocd-apps/values-teleport-kube-agent.yaml.tmpl",

		// For KubeAid Agent.
		"argocd-apps/templates/teleport-kube-agent.yaml.tmpl",
		"argocd-apps/values-teleport-kube-agent.yaml.tmpl",
	}

	CustomerSpecificSecretTemplateNames = []string{
		"sealed-secrets/obmondo/teleport-kube-agent-join-token.yaml.tmpl",
	}
)

// Config template names.
var (
	TemplateNameAWSGeneralConfig = "templates/aws/general.config.yaml.tmpl"
	TemplateNameAWSSecretsConfig = "templates/aws/secrets.config.yaml.tmpl"

	TemplateNameAzureGeneralConfig = "templates/azure/general.config.yaml.tmpl"
	TemplateNameAzureSecretsConfig = "templates/azure/secrets.config.yaml.tmpl"

	TemplateNameHetznerHCloudGeneralConfig = "templates/hetzner/hcloud/general.config.yaml.tmpl"
	TemplateNameHetznerHCloudSecretsConfig = "templates/hetzner/hcloud/secrets.config.yaml.tmpl"

	TemplateNameHetznerBareMetalGeneralConfig = "templates/hetzner/bare-metal/general.config.yaml.tmpl"
	TemplateNameHetznerBareMetalSecretsConfig = "templates/hetzner/bare-metal/secrets.config.yaml.tmpl"

	TemplateNameHetznerHybridGeneralConfig = "templates/hetzner/hybrid/general.config.yaml.tmpl"
	TemplateNameHetznerHybridSecretsConfig = "templates/hetzner/hybrid/secrets.config.yaml.tmpl"

	TemplateNameLocalGeneralConfig = "templates/local/general.config.yaml.tmpl"
	TemplateNameLocalSecretsConfig = "templates/local/secrets.config.yaml.tmpl"
)

// Miscellaneous.
const (
	TemplateNameK3DConfig = "templates/k3d.config.yaml.tmpl"

	// For KubePrometheus.
	TemplateNameKubePrometheusArgoCDApp = "argocd-apps/templates/kube-prometheus.yaml.tmpl"
	TemplateNameKubePrometheusVars      = "cluster-vars.jsonnet.tmpl"
)
