// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package constants

const TemplateNameStoragePlanExecutor = "templates/storage-plan-executor.sh.tmpl"

// Common template names.
var (
	CommonNonSecretTemplateNames = []string{
		// For KubeAid Bootstrap Script general config.
		"kubeaid-cli.general.yaml.tmpl",

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
		"sealed-secrets/argocd/repo-kubeaid-config.yaml.tmpl",
	}

	KubeaidDeployKeySecretTemplateName = "sealed-secrets/argocd/repo-kubeaid.yaml.tmpl"
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
		// For CrossPlane.
		"argocd-apps/templates/crossplane.yaml.tmpl",
		"argocd-apps/values-crossplane.yaml.tmpl",
		"argocd-apps/templates/crossplane-providers-and-functions.yaml.tmpl",
		"argocd-apps/values-crossplane-providers-and-functions.yaml.tmpl",
		"argocd-apps/templates/crossplane-compositions.yaml.tmpl",
		"argocd-apps/values-crossplane-compositions.yaml.tmpl",
		"argocd-apps/templates/infrastructure.yaml.tmpl",
		"infrastructure/azure/workload-identity-infrastructure.yaml.tmpl",

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
		// For CrossPlane.
		"sealed-secrets/crossplane/azure-credentials.yaml.tmpl",

		// For ClusterAPI.
		"sealed-secrets/capi-cluster/service-account-issuer-keys.yaml.tmpl",
	}

	AzureDisasterRecoverySpecificNonSecretTemplateNames = []string{
		// For CrossPlane.
		"infrastructure/azure/disaster-recovery-infrastructure.yaml.tmpl",

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

		// For postfinance kubelet-csr-approver. The values template
		// reads TemplateValues.HetznerBareMetalHostPublicIPs, which
		// getTemplateValues populates via a one-shot Robot API call
		// for every bare-metal Hetzner host's main IP.
		"argocd-apps/templates/kubelet-csr-approver.yaml.tmpl",
		"argocd-apps/values-kubelet-csr-approver.yaml.tmpl",

		// For Rook CEPH.
		"argocd-apps/templates/rook-ceph.yaml.tmpl",
		"argocd-apps/values-rook-ceph.yaml.tmpl",
	}

	HetznerBareMetalSpecificSecretTemplateNames = []string{
		// For Cluster API.
		"sealed-secrets/capi-cluster/hetzner-ssh-keypair.yaml.tmpl",
	}
)

// Bare metal specific template names.

const KubeOneConfigTemlateName = "kubeone/kubeone-cluster.yaml.tmpl"

var BareMetalSpecificNonSecretTemplateNames = []string{
	// For KubeOne.
	KubeOneConfigTemlateName,

	// For Cilium
	"argocd-apps/templates/cilium.yaml.tmpl",
	"argocd-apps/values-cilium.yaml.tmpl",

	// For OpenEBS dynamic LocalPV provisioner.
	"argocd-apps/templates/localpv-provisioner.yaml.tmpl",
	"argocd-apps/values-localpv-provisioner.yaml.tmpl",
}

// Traefik. The ingress controller in front of NetBird Mgmt (and
// Keycloak when managed); cert-manager's http01 solver also targets
// its ingressClassName (see values-cert-manager.yaml.tmpl).
// Sync-order 15 keeps it ahead of any chart that creates Ingress
// objects.
var TraefikTemplateNames = []string{
	"argocd-apps/templates/traefik.yaml.tmpl",
	"argocd-apps/values-traefik.yaml.tmpl",
}

// CloudNativePG operator. Provides the Cluster CRD that the
// keycloakx chart (when managed Keycloak is enabled) and the
// netbird chart both instantiate — for keycloak-pgsql and
// netbird-pgsql respectively.
var CloudNativePGTemplateNames = []string{
	"argocd-apps/templates/cloudnative-pg.yaml.tmpl",
	"argocd-apps/values-cloudnative-pg.yaml.tmpl",
}

// NetBird Mgmt + Signal + Relay + Dashboard + Coturn — the full
// VPN mesh stack. Sync-order 25 keeps it after cnpg (so the
// netbird-pgsql Cluster CR the chart's kubeaid-addons subdep
// instantiates can land), traefik (so its ingressClassName
// resolves), and keycloakx when managed (so the realm + OIDC
// clients the post-sync gocloak reconcile creates exist before
// NetBird Mgmt does its first OIDC handshake).
var NetBirdNonSecretTemplateNames = []string{
	"argocd-apps/templates/netbird.yaml.tmpl",
	"argocd-apps/values-netbird.yaml.tmpl",
}

// NetBird Kubernetes Operator. Rendered on workload clusters that
// opted into Keycloak login (cluster.type=workload AND
// cluster.keycloak set) and on VPN clusters, so the cluster has the
// operator + CRDs available for operator-applied NetworkRouter /
// NetworkResource / NBPolicy resources. The values overlay renders
// managementURL (cluster.netbird.dns on VPN clusters; the
// netbird.<base> Keycloak-DNS convention on workload clusters); the
// Mgmt API token comes from the paired SealedSecret below.
// Sync-order 10 — early, so the CRDs land before any
// operator-applied resource.
var NetBirdOperatorTemplateNames = []string{
	"argocd-apps/templates/netbird-operator.yaml.tmpl",
	"argocd-apps/values-netbird-operator.yaml.tmpl",
}

// CertManagerCloudflareAPITokenSecretTemplateName seals secrets.yaml's
// acme.cloudflareApiToken into the cert-manager/cloudflare-api-token
// Secret the DNS-01 ClusterIssuer's solver references. Only registered
// when cluster.acmeDNS01 is set (parser validation guarantees the
// token is present by then).
const CertManagerCloudflareAPITokenSecretTemplateName = "sealed-secrets/cert-manager/cloudflare-api-token.yaml.tmpl"

// NetBirdOperatorAPIKeySecretTemplateName seals secrets.yaml's
// netbird.apiKey (a Mgmt service-user access token) into the
// netbird/netbird-mgmt-api-key Secret the operator Deployment's
// NB_API_KEY env reads (the chart's default secret ref). Only
// registered when the operator is rendered AND the key is present —
// when absent, bootstrap pauses at awaitNetBirdOperatorToken with
// create-it-manually instructions instead.
const NetBirdOperatorAPIKeySecretTemplateName = "sealed-secrets/netbird/netbird-mgmt-api-key.yaml.tmpl"

// Managed-Keycloak template names. Included only when
// cluster.type=vpn AND cluster.keycloak.mode=managed — kubeaid-cli
// installs Keycloak via the keycloakx Helm chart on this cluster.
// Backed by CNPG Postgres; ingress exposes cluster.keycloak.dns
// publicly so kubelogin and end-users can reach the realm endpoints.
// Sync-order 20.
var KeycloakManagedNonSecretTemplateNames = []string{
	"argocd-apps/templates/keycloakx.yaml.tmpl",
	"argocd-apps/values-keycloakx.yaml.tmpl",
}

// Managed-Keycloak SealedSecrets — only when kubeaid-cli installs
// Keycloak itself. Seeds Keycloak's initial admin password
// (consumed by the keycloakx chart's pre-install hook).
var KeycloakManagedSecretTemplateNames = []string{
	"sealed-secrets/keycloakx/keycloak-admin.yaml.tmpl",
}

// VPN-cluster NetBird SealedSecrets — both modes.
//   - netbird: holds every credential the NetBird Helm chart's
//     envFromSecret block references — OIDC client IDs/secret,
//     datastoreEncryptionKey, relayPassword, stun/turn server URLs,
//     turn user/password. kubeaid-cli pre-generates the random
//     keys and read-or-generates them on re-runs so the same value
//     stays put across bootstraps. The OIDC client secret is the
//     same plaintext ReconcileNetBird passes to Keycloak when
//     managed, or read from secrets.yaml when external (operator
//     supplies the value out-of-band).
//   - netbird-turn-credentials: Coturn server reads this for its
//     own TURN auth. Password matches the netbird Secret's
//     turnServerPassword so Mgmt's hand-back to clients lines up.
var NetBirdSecretTemplateNames = []string{
	"sealed-secrets/netbird/netbird.yaml.tmpl",
	"sealed-secrets/netbird/netbird-turn-credentials.yaml.tmpl",
}

// Obmondo customer specific template names.
var (
	// For KubeAid Agent. Included whenever obmondo.monitoring is true.
	KubeAidAgentNonSecretTemplateNames = []string{
		"argocd-apps/templates/kubeaid-agent.yaml.tmpl",
		"argocd-apps/values-kubeaid-agent.yaml.tmpl",
	}

	// mTLS client cert issued by Obmondo. The same cert+key pair is rendered
	// into two namespaces — obmondo for kubeaid-agent (Obmondo API auth) and
	// monitoring for kube-prometheus Alertmanager (pushing alerts to
	// Obmondo's alert-receiver). Included whenever obmondo.monitoring is true.
	ObmondoClientCertSecretTemplateNames = []string{
		"sealed-secrets/obmondo/obmondo-clientcert.yaml.tmpl",
		"sealed-secrets/monitoring/obmondo-clientcert.yaml.tmpl",
	}

	// Alertmanager's main config Secret, with the runtime alertmanager.yaml
	// routing alerts to Obmondo's alert receiver. Included whenever
	// obmondo.monitoring is true.
	AlertmanagerMainSecretTemplateName = "sealed-secrets/monitoring/alertmanager-main.yaml.tmpl"
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

	TemplateNameBareMetalGeneralConfig = "templates/bare-metal/general.config.yaml.tmpl"
	TemplateNameBareMetalSecretsConfig = "templates/bare-metal/secrets.config.yaml.tmpl"

	TemplateNameLocalGeneralConfig = "templates/local/general.config.yaml.tmpl"
	TemplateNameLocalSecretsConfig = "templates/local/secrets.config.yaml.tmpl"
)

const TemplateNameK3DConfig = "templates/k3d.config.yaml.tmpl"

// For KubePrometheus.
const (
	TemplateNameKubePrometheusArgoCDApp = "argocd-apps/templates/kube-prometheus.yaml.tmpl"
	TemplateNameKubePrometheusVars      = "cluster-vars.jsonnet.tmpl"
)
