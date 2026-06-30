// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package constants

import (
	"path"
	"time"
)

const TempDirectory = "/tmp/kubeaid-core"

// Environment variable names.
const (
	EnvNameSSHAuthSock   = "SSH_AUTH_SOCK"
	EnvNameSSHKnownHosts = "SSH_KNOWN_HOSTS"

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

	FlagNameKubeAidVersion = "kubeaid-version"

	FlagNameManagementClusterName = "management-cluster-name"

	// ManagementClusterNamePrefix is prepended to the target cluster name when the operator
	// does not supply --management-cluster-name explicitly. The resulting name
	// (e.g. "mgmt-staging") scopes the local k3d bootstrap cluster to the
	// target cluster, preventing a second bootstrap run — or a bootstrap of a different
	// target cluster — from silently reusing a stale k3d cluster with leftover Cluster API
	// state. Operators who supply the flag explicitly retain full control.
	ManagementClusterNamePrefix = "mgmt-"

	FlagNameConfigsDirectory             = "configs-directory"
	FlagNameConfigsDirectoryDefaultValue = "outputs/configs"

	FlagNameSkipMonitoringSetup = "skip-monitoring-setup"
	FlagNameSkipPRWorkflow      = "skip-pr-workflow"
	FlagNameSkipClusterctlMove  = "skip-clusterctl-move"

	FlagNameAWSAccessKeyID     = "aws-access-key-id"
	FlagNameAWSSecretAccessKey = "aws-secret-access-key"
	FlagNameAWSSessionToken    = "aws-session-token"
	FlagNameAWSRegion          = "aws-region"
	FlagNameAMIID              = "ami-id"

	FlagNameAzureClientSecret = "azure-client-secret"
	FlagNameNewImageOffer     = "new-image-offer"

	FlagNameHetznerAPIToken      = "hetzner-cloud-api-token"
	FlagNameHetznerRobotUsername = "hetzner-robot-username"
	FlagNameHetznerRobotPassword = "hetzner-robot-password"
	FlagNameNewImageName         = "new-image-name"
	FlagNameNewImagePath         = "new-image-path"

	FlagNameNewK8sVersion = "new-k8s-version"

	FlagNameOSSize      = "os-size"
	FlagNameZFSPoolSize = "zfs-pool-size"
)

// Kube API server CLI flags.
const (
	KubeAPIServerFlagAuditPolicyFile = "audit-policy-file"
	KubeAPIServerFlagAuditLogPath    = "audit-log-path"

	// AuthenticationConfiguration delivery (k8s 1.30+). The OIDC
	// block in cluster.apiServer.oidc is rendered into this YAML
	// file and kube-apiserver is pointed at it via the
	// --authentication-config flag. The legacy --oidc-* flags are
	// no longer emitted.
	KubeAPIServerFlagAuthenticationConfig = "authentication-config"
	KubeAPIServerAuthenticationConfigPath = "/etc/kubernetes/auth-config.yaml"

	// Obmondo's central Keycloak for SRE access. Trust is added
	// to customer kube-apiservers as a SECOND jwt: entry in the
	// AuthenticationConfiguration when obmondo.monitoring is on,
	// so Obmondo SRE users can kubectl into a customer cluster
	// without the customer issuing them an account in their own
	// Keycloak. One-way: customer's Keycloak is unaware of
	// Obmondo's, no IdP federation.
	//
	// Note the "/auth" base path and the "Obmondo" realm casing: this is
	// the realm's canonical issuer (its discovery document's "issuer"
	// field), and kube-apiserver matches the token's "iss" against it
	// byte-for-byte — a mismatch is a 401, not a warning.
	ObmondoKeycloakIssuerURL = "https://keycloak.obmondo.com/auth/realms/Obmondo"
)

// Cloud providers.
const (
	CloudProviderAWS       = "aws"
	CloudProviderHetzner   = "hetzner"
	CloudProviderAzure     = "azure"
	CloudProviderBareMetal = "bare-metal"
	CloudProviderLocal     = "local"
)

// Disk types.
const (
	DiskTypeHDD  = "HDD"
	DiskTypeSSD  = "SSD"
	DiskTypeNVMe = "NVMe"

	DiskTypeUnknown = "Unknown"
)

const HighSpeedNICThreshold = 5000 // GBPS.

const OSDefaultSize = 50 // GB.

// ZFS.
const (
	ZFSPoolDefaultSize = (ZFSVolumeSizeContainerImages + ZFSVolumeSizePodLogs + ZFSVolumeSizePodEphemeralVolumes) + 20 // = 220 GB.

	ZFSVolumeSizeContainerImages     = 100
	ZFSVolumeSizePodLogs             = 50
	ZFSVolumeSizePodEphemeralVolumes = 50
)

const CEPHNodeMinSize = 50 // GB.

// Output paths.
var (
	OutputsDirectory = "outputs"

	OutputLogsDirectory = path.Join(OutputsDirectory, "logs")

	OutputPathKnownHostsFile = path.Join(TempDirectory, "known_hosts")

	OutputPathManagementClusterK3DConfig = path.Join(OutputsDirectory, "k3d.config.yaml")

	OutputPathManagementClusterHostKubeconfig = path.Join(
		OutputsDirectory,
		"kubeconfigs/clusters/management/host.yaml",
	)
	OutputPathManagementClusterContainerKubeconfig = path.Join(
		OutputsDirectory,
		"kubeconfigs/clusters/management/container.yaml",
	)

	OutputPathMainClusterKubeconfig = path.Join(OutputsDirectory, "kubeconfigs/clusters/main.yaml")

	OutputPathJWKSDocument = path.Join(
		OutputsDirectory,
		"workload-identity/openid-provider/jwks.json",
	)
)

// ArgoCD.
const (
	ReleaseNameArgoCD = "argocd"

	ArgoCDProjectKubeAid   = "kubeaid"
	ArgoCDRoleKubeAidAgent = "kubeaid-agent"

	// Apps.
	ArgoCDAppArgoCD             = "argocd"
	ArgoCDAppRoot               = "root"
	ArgoCDAppSealedSecrets      = "sealed-secrets"
	ArgoCDAppCapiCluster        = "capi-cluster"
	ArgoCDAppHetznerRobot       = "hetzner-robot"
	ArgoCDAppClusterAutoscaler  = "cluster-autoscaler"
	ArgoCDAppVelero             = "velero"
	ArgoCDAppKubePrometheus     = "kube-prometheus"
	ArgoCDExternalSnapshotter   = "external-snapshotter"
	ArgoCDAppCilium             = "cilium"
	ArgoCDAppAzureDiskCSIDriver = "azuredisk-csi-driver"
	ArgoCDAppHCloudCSIDriver    = "hcloud-csi-driver"
	ArgoCDAppRookCeph           = "rook-ceph"
	ArgoCDAppLocalPVProvisioner = "localpv-provisioner"
	ArgoCDAppCCMHCloud          = "ccm-hcloud"
	ArgoCDAppCCMHetzner         = "ccm-hetzner"
	ArgoCDAppTraefik            = "traefik"

	ArgoCDProjectRolePolicyFmt = "p, proj:%s:%s, %s, %s, %s/*, %s" // Inputs: project-name, role-name, resource, action, project-name, effect
	ArgoCDLabelKeyManagedBy    = "kubeaid.io/managed-by"

	ArgoCDRBACEffectAllow = "allow"
	ArgoCDRBACEffectDeny  = "deny"

	ArgoCDProjectRoleSecretName = "argocd-project-role-kubeaid-agent"
)

// Sealed Secrets.
const (
	ReleaseNameSealedSecrets    = "sealed-secrets"
	SealedSecretsControllerName = ReleaseNameSealedSecrets + "-controller"

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
)

// Hetzner
const (
	HetznerModeBareMetal = "bare-metal"
	HetznerModeHCloud    = "hcloud"
	HetznerModeHybrid    = "hybrid"

	HetznerRobotWebServiceAPI = "https://robot-ws.your-server.de"

	HetznerNetworkCIDR       = "10.0.0.0/16"
	HCloudServersSubnetCIDR  = "10.0.0.0/24"
	HetznerVSwitchSubnetCIDR = "10.0.1.0/24"

	HCloudServerTypeCPX22 = "cpx22"

	HCloudServerImageUbuntu2404 = "ubuntu-24.04"

	HCloudLocationHel1 = "hel1"
	HCloudLocationFsn1 = "fsn1"
	HCloudLocationNbg1 = "nbg1"
	HCloudLocationAsh  = "ash"

	HCloudLBTypeLB11 = "lb11"

	// Hetzner Bare Metal Server (HBMS) OS installation via Hetzner Robot (HRobot).
	HRobotResetTypeHardware = "hw"
	// Pinned to the latest Ubuntu LTS so that every new HBMS receives current security patches.
	// Bump this constant when a newer LTS becomes available in the HRobot catalogue.
	HBMSInstallDistributionLatestUbuntu = "Ubuntu 24.04 LTS base"
	HBMSOSInstallationPollInterval      = 20 * time.Second
	// HBMSOSInstallationMaxWaitTime is the per-server upper bound the
	// post-reset SSH probe waits for the freshly-installed OS to come
	// up. Hetzner installimage takes 8-15 min on normal hardware
	// (1-3 min reset → rescue boot, 5-10 min partition + debootstrap
	// + first-boot package install, 1-2 min first boot + sshd up).
	// 20 min absorbs the slow tail (HDD instead of NVMe, apt mirror
	// in a busier DC, wipeDisks=true triggering secure-erase before
	// partitioning) with margin to spare — dying mid-bootstrap is
	// worse than waiting another few minutes on an install that
	// eventually completes. Don't unbump without a corresponding
	// investigation note.
	HBMSOSInstallationMaxWaitTime = 20 * time.Minute
)

// HCloudNATGatewayLocations is the ordered list of HCloud locations
// kubeaid-cli tries when placing the NAT gateway server. cx23 is a
// cost-optimized / limited-availability type (Intel/AMD x86), so
// stock can be uneven per datacenter; fall through to the next
// location when Hetzner returns resource_unavailable.
var HCloudNATGatewayLocations = []string{
	HCloudLocationHel1,
	HCloudLocationFsn1,
	HCloudLocationNbg1,
}

const (
	// Namespaces.
	NamespaceArgoCD        = "argocd"
	NamespaceObmondo       = "obmondo"
	NamespaceVelero        = "velero"
	NamespaceSealedSecrets = "sealed-secrets"
	NamespaceCrossPlane    = "crossplane"
	NamespaceCilium        = "cilium"
	NamespaceCiliumTest    = "cilium-test"
	NamespaceKeycloak      = "keycloakx"
	NamespaceCloudNativePG = "cnpg-operator"
	NamespaceNetBird       = "netbird"
	NamespaceTraefik       = "traefik"
	NamespaceKubeSystem    = "kube-system"
	NamespaceCertManager   = "cert-manager"

	// SecretNameCloudCredentials is the Secret the HCloud Cloud
	// Controller Manager, HCloud CSI driver, and Cluster Autoscaler
	// all read for their Hetzner API token. kubeaid-cli pre-creates it
	// directly on the main cluster during bootstrap so the CCM can
	// start before sealed-secrets-controller is up — see
	// pkg/core/hcloud_credentials.go for the chicken-and-egg this
	// breaks. The same SealedSecret is also rendered into
	// kubeaid-config so the cluster state remains declaratively
	// captured for DR.
	SecretNameCloudCredentials = "cloud-credentials"

	// keycloakx Service inside NamespaceKeycloak. kubeaid-cli
	// port-forwards to this Service during bootstrap to call the
	// admin API, mirroring the existing argocd-server pattern.
	ServiceNameKeycloakx = "keycloakx-http"
	ServicePortKeycloakx = 80

	// ArgoCDAppKeycloakx is the name of the keycloakx ArgoCD App
	// kubeaid-cli waits for Healthy before calling the admin API.
	ArgoCDAppKeycloakx = "keycloakx"

	// ArgoCDAppNetbird is the name of the netbird ArgoCD App.
	ArgoCDAppNetbird = "netbird"

	// ArgoCDAppCertManager is the name of the cert-manager ArgoCD App.
	ArgoCDAppCertManager = "cert-manager"

	// ArgoCDAppCloudNativePG is the name of the cloudnative-pg
	// (CNPG) ArgoCD App. It installs the operator + the
	// postgresql.cnpg.io/v1 Cluster/Pooler CRDs that keycloakx
	// (in managed mode, for keycloak-pgsql) and netbird (for
	// netbird-pgsql) both instantiate during their own sync, so
	// the bootstrap syncs it ahead of those apps.
	ArgoCDAppCloudNativePG = "cloudnative-pg"

	// Keycloak admin Secret keys. Names match what the keycloakx
	// chart's pre-install hook reads.
	SecretNameKeycloakAdmin   = "keycloak-admin"
	SecretKeyKeycloakUsername = "username"
	SecretKeyKeycloakPassword = "KEYCLOAK_PASSWORD"
	KeycloakAdminUsername     = "admin"

	// netbird Secret. Holds every plaintext credential the NetBird
	// Helm chart envFroms: the OIDC client ID/secret pair pointing
	// at the netbird-backend Keycloak client, the symmetric AES key
	// the Mgmt server uses to encrypt its datastore, the Relay
	// shared secret, and the static turn user/password Coturn and
	// Mgmt agree on. kubeaid-cli read-or-generates each random key
	// against the cluster-side Secret so re-runs don't drift.
	SecretNameNetBird             = "netbird"
	SecretKeyNetBirdIDPClientID   = "idpClientID"
	SecretKeyNetBirdIDPMgmtID     = "idpClientMgmtID"
	SecretKeyNetBirdIDPMgmtSecret = "idpClientMgmtSecret"
	SecretKeyNetBirdIDPSAUser     = "idpServiceAccountUser"
	SecretKeyNetBirdDatastoreKey  = "datastoreEncryptionKey"
	SecretKeyNetBirdRelayPassword = "relayPassword"
	SecretKeyNetBirdStunServer    = "stunServer"
	SecretKeyNetBirdTurnServer    = "turnServer"
	SecretKeyNetBirdTurnUser      = "turnServerUser"
	SecretKeyNetBirdTurnPassword  = "turnServerPassword"
	SecretKeyNetBirdPostgresDSN   = "postgresDSN"
	NetBirdClientID               = "netbird-client"
	NetBirdBackendClientID        = "netbird-backend"

	// netbird-turn-credentials Secret. Coturn server reads this for
	// its own TURN auth via existingSecret in the chart values; the
	// password must match SecretKeyNetBirdTurnPassword above so
	// Mgmt's hand-back to clients lines up with what Coturn actually
	// authenticates against.
	SecretNameNetBirdTurnCredentials = "netbird-turn-credentials"
	SecretKeyNetBirdTurnCredsUser    = "username"
	SecretKeyNetBirdTurnCredsPwd     = "password"

	// 32 bytes -> 256-bit AES key after base64 decode. Matches the
	// length of NetBird Mgmt's datastoreEncryptionKey field.
	NetBirdDatastoreKeyByteLen = 32

	// Service Accounts.
	ServiceAccountCAPZ          = "capz-manager"
	ServiceAccountASO           = "azureserviceoperator-default"
	ServiceAccountVelero        = "velero"
	ServiceAccountSealedSecrets = "sealed-secrets"
)

const PEMBlockTypeOpenSSHPrivateKey = "OPENSSH PRIVATE KEY"

// Cluster types.
const (
	ClusterTypeManagement = "management"
	ClusterTypeMain       = "main"

	ClusterTypeVPN      = "vpn"
	ClusterTypeWorkload = "workload"

	// Keycloak modes for cluster.type=vpn clusters.
	//   managed:  kubeaid-cli installs Keycloak via the keycloakx
	//             chart on this cluster, generates the admin
	//             password, and runs the gocloak realm reconciler.
	//   external: operator's existing Keycloak; kubeaid-cli only
	//             configures kube-apiserver / NetBird Mgmt to trust
	//             it. The realm + clients must be set up by hand
	//             (see argocd-helm-charts/netbird/README.md, "Keycloak
	//             realm prerequisites") before bootstrap.
	KeycloakModeManaged  = "managed"
	KeycloakModeExternal = "external"
)

// Miscellaneous.
const (
	RepoURLObmondoKubeAid = "https://github.com/Obmondo/KubeAid"

	// Public HTTPS URL for KubeAid — used by ArgoCD (read-only, no deploy key needed).
	KubeAidPublicHTTPSURL = "https://github.com/Obmondo/KubeAid.git"

	// GitHub API URL for listing KubeAid releases (used to pick latest-1).
	KubeAidReleasesAPIURL = "https://api.github.com/repos/Obmondo/KubeAid/releases"

	// Local docker image tag kubeaid-cli builds on first
	// buildKubePrometheus invocation. Holds the small jsonnet
	// toolchain (jsonnet, jb, gojsontoyaml) that runs build.sh
	// without requiring those binaries on the host. Tag is
	// stable; the image is built from the embedded Dockerfile in
	// pkg/core, so docker layer caching makes repeat builds free.
	KubePromBuilderImage = "kubeaid-cli/kube-prom-builder:latest"

	GzippedFilenameSuffix = ".gz"
)

// Time durations
const (
	OneDay   = 24 * time.Hour
	OneMonth = 30 * OneDay
)

// Git related.
const (
	CommitAuthorName  = "KubeAid CLI"
	CommitAuthorEmail = "info@obmondo.com"
)

// Docker related.
const (
	DockerSocketPath         = "/var/run/docker.sock"
	DockerDefaultNetworkName = "default"
)

// K3s related.
const (
	K3sReleasesAPIURL = "https://api.github.com/repos/k3s-io/k3s/releases/latest"

	// CGroup v1 support has been dropped from K8s version v1.35.
	// REFER : https://www.sysdig.com/blog/kubernetes-1-35-whats-new#changes-in-kubernetes-135-that-may-break-things.
	MaxCGroupV1CompatibleK3sVersion = "v1.34.5-k3s1"
)

// K8s version related
const (
	MinSupportedK8sVersion = "v1.30"
	//
	// Whatever is the latest K8s version, that becomes the max supported K8s version.
	// We get the latest K8s version from the K8s release API.
	K8sReleaseAPIURL = "https://dl.k8s.io/release/stable.txt"

	// URL pattern for fetching the latest patch of a specific minor version.
	// Use fmt.Sprintf with the minor version number, e.g. fmt.Sprintf(K8sStableMinorURLFmt, 34)
	// yields "https://dl.k8s.io/release/stable-1.34.txt".
	K8sStableMinorURLFmt = "https://dl.k8s.io/release/stable-1.%d.txt"

	// CGroup v1 support has been dropped from K8s version v1.35.
	// REFER : https://www.sysdig.com/blog/kubernetes-1-35-whats-new#changes-in-kubernetes-135-that-may-break-things.
	MaxCGroupV1CompatibleK8sVersion = "v1.34"

	// For the Bare Metal provider though, the story is a bit different.
	// We're using KubeOne v1.12. And you can see the K8s versions officially supported by KubeOne
	// here : https://docs.kubermatic.com/kubeone/v1.12/architecture/compatibility/supported-versions.
	// That range becomes the range of K8s version supported by KubeAid CLI.
	// NOTE : We need update this range manually, when upgrading KubeOne.
	MinKubeOneSupportedK8sVersion = "v1.32"
	MaxKubeOneSupportedK8sVersion = "v1.34"
)

// Kubernetes -> KubePrometheus compatibility matrix.
// This makes it easy to select a default KubePrometheus version for a given K8s version.
// REFER : https://github.com/prometheus-operator/kube-prometheus?tab=readme-ov-file#compatibility.
var KubernetesKubePrometheusVersionCompatibilityMatrix = map[string][]string{
	"v1.32": {"v0.16.0"},
	"v1.33": {"v0.16.0", "v0.17.0"},
	"v1.34": {"v0.16.0", "v0.17.0"},
	"v1.35": {"v0.17.0"},
}
