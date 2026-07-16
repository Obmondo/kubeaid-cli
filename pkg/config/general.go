// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package config

import (
	coreV1 "k8s.io/api/core/v1"

	"github.com/Obmondo/kubeaid-cli/pkg/storageplanner/storageplan"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/giturl"
)

var (
	GeneralConfigFileContents []byte

	ParsedGeneralConfig = &GeneralConfig{}
	ParsedSecretsConfig = &SecretsConfig{}
)

type (
	// Non secret configuration options.
	GeneralConfig struct {
		// Git server specific details.
		Git GitConfig `yaml:"git"`

		// KubeAid and KubeAid Config repository specific details.
		// The KubeAid and KubeAid Config repositories must be hosted in the same Git server.
		Forks ForksConfig `yaml:"forkURLs" validate:"required"`

		// Kubernetes specific details.
		Cluster ClusterConfig `yaml:"cluster" validate:"required"`

		// Cloud provider specific details.
		Cloud CloudConfig `yaml:"cloud" validate:"required"`

		// Kube Prometheus installation specific details.
		KubePrometheus *KubePrometheusConfig `yaml:"kubePrometheus"`

		// KubeaidStoragectl pins the kubeaid-storagectl release tag
		// used by the bare-metal preKubeadm script when carving the
		// ZFS pool and Ceph partition. Leave nil (block omitted) to
		// fall back to the kubeaid-cli binary's own release version,
		// which is the right default for most operators — every node
		// downloads the storagectl that ships with the kubeaid-cli
		// release that bootstrapped it. Set explicitly to override:
		//
		//   - to pin against a tag newer/older than kubeaid-cli for
		//     testing a fix or rolling back, or
		//   - to point at an unreleased dev build when running a
		//     `go run ./cmd/kubeaid-cli` development bootstrap (the
		//     CLI's KubeaidCLIVersion is empty there and the chart
		//     would otherwise fall through to `latest`, which 404s if
		//     no release has been published yet).
		KubeaidStoragectl *KubeaidStoragectlConfig `yaml:"kubeaidStoragectl"`

		// Obmondo customer specific details.
		Obmondo *ObmondoConfig `yaml:"obmondo"`
	}

	// KubeaidStoragectlConfig pins the kubeaid-storagectl release.
	// See GeneralConfig.KubeaidStoragectl for when to set it.
	KubeaidStoragectlConfig struct {
		// Version is the GitHub release tag of kubeaid-storagectl —
		// rendered into the chart as `global.kubeaidStoragectl.version`
		// and used to build the `releases/download/<version>/` URL the
		// node's preKubeadm wget hits. Empty string is treated as "not
		// set" and falls back to kubeaid-cli's own version, same as
		// omitting the parent block.
		Version string `yaml:"version"`
	}

	// Git specific details, used by KubeAid CLI,
	// to clone repositories from and push changes to the Git server.
	// We enforce the user to use SSH, for authenticating to the Git server.
	GitConfig struct {
		CABundlePath string `yaml:"caBundlePath"`
		CABundle     []byte

		// SSH username.
		SSHUsername string `yaml:"sshUsername" validate:"notblank" default:"git"`

		// Either a private key file path or useSSHAgent=true. The
		// embedded struct supplies both yaml fields plus the
		// hydrated PublicKey/Fingerprint, so a YubiKey-backed agent
		// flow and a file-backed flow share one schema.
		*SSHKeyPairConfig `yaml:",inline"`

		// Additional SSH known hosts.
		// Merged with known hosts of common Git repo hosting providers (like Azure DevOps, GitLab etc.)
		KnownHosts []string `yaml:"knownHosts"`
	}

	// KubeAid and KubeAid Config repository specific details.
	// We require the KubeAid and KubeAid Config repositories to be hosted in the same Git server.
	ForksConfig struct {
		// KubeAid repository specific details.
		KubeaidFork KubeAidForkConfig `yaml:"kubeaid" validate:"required"`

		// KubeAid Config repository specific details.
		KubeaidConfigFork KubeaidConfigForkConfig `yaml:"kubeaidConfig" validate:"required"`
	}

	// KubeAid repository specific details.
	KubeAidForkConfig struct {
		// KubeAid repository SSH URL.
		URL       string `yaml:"url" validate:"required"`
		ParsedURL *giturl.ParsedURL

		// KubeAid git ref (tag / branch / commit).
		Version string `yaml:"version"`
	}

	// KubeAid Config repository specific details.
	KubeaidConfigForkConfig struct {
		// KubeAid Config repository SSH URL.
		URL       string `yaml:"url" validate:"required"`
		ParsedURL *giturl.ParsedURL

		// Name of the directory inside your KubeAid Config repository's k8s folder, where the KubeAid
		// Config files for this cluster will be contained.
		//
		// When not specified, the directory name will default to the cluster name.
		//
		// So, suppose your cluster name is 'staging'. Then, the directory name will default to
		// 'staging'. Or you can customize it to something like 'staging.qa'.
		Directory string `yaml:"directory"`
	}

	ClusterConfig struct {
		Type string `yaml:"type" validate:"notblank,oneof=vpn workload" default:"workload"`

		// Name of the Kubernetes cluster.
		//
		// We don't allow using dots in the cluster name, since it can cause issues with tools like
		// ClusterAPI and Cilium : which use the cluster name to generate other configurations.
		Name string `yaml:"name" validate:"notblank"`

		// Kubernetes version (>= 1.30.0).
		K8sVersion string `yaml:"k8sVersion" validate:"notblank"`

		// Whether you would like to enable Kubernetes Audit Logging out of the box.
		// Suitable Kubernetes API configurations will be done for you automatically. And they can be
		// changed using the apiSever struct field.
		EnableAuditLogging bool `yaml:"enableAuditLogging" default:"True"`

		// ACMEEmail is the contact email used to register with the ACME
		// CA (Let's Encrypt) when cert-manager's ClusterIssuer is
		// rendered. Required when cluster.keycloak.mode=managed (the
		// keycloakx and netbird-mgmt Ingresses both need TLS certs);
		// optional otherwise. Used as Issuer.spec.acme.email.
		ACMEEmail string `yaml:"acmeEmail" validate:"omitempty,email"`

		// ACMEDNS01 switches the rendered ClusterIssuer's solver from
		// the HTTP-01 default to DNS-01. Required for the split-horizon
		// mesh pattern: NetBird-exposed services use real public DNS
		// names (e.g. argocd.staging.acme.com) that only resolve inside
		// the mesh — Let's Encrypt can never reach them over HTTP, but
		// proves ownership via a TXT record on the public zone instead.
		// Requires cluster.acmeEmail plus the provider credential in
		// secrets.yaml (acme.cloudflareApiToken).
		ACMEDNS01 *ACMEDNS01Config `yaml:"acmeDNS01"`

		// Configuration options for the Kubernetes API server.
		APIServer APIServerConfig `yaml:"apiServer"`

		// Lockdown pre-answers the end-of-bootstrap Host Firewall (CCNP)
		// step. nil = ask interactively (legacy behavior); true = apply
		// without prompting (CI-safe); false = skip the step.
		Lockdown *bool `yaml:"lockdown"`

		// Keycloak declares the Keycloak instance a VPN cluster hosts as
		// NetBird's SSO IdP. Required on cluster.type=vpn (mode=managed →
		// kubeaid-cli installs it; mode=external → operator runs it
		// elsewhere). Not supported on workload clusters — access there is
		// via the NetBird mesh (cluster.netbird.dns), so a keycloak block
		// on a workload cluster is rejected.
		Keycloak *KeycloakConfig `yaml:"keycloak"`

		// NetBird declares the NetBird Management instance this VPN
		// cluster hosts. Only meaningful when cluster.type=vpn AND
		// cluster.keycloak.mode=managed. NetBird Mgmt's OIDC client
		// is created in the same Keycloak realm; its public DNS is
		// used for the redirect URI and audience claim.
		NetBird *NetBirdConfig `yaml:"netbird"`

		// Other than the root user, addtional users that you would like to be created in each node.
		// NOTE : Currently, we can't register additional SSH key-pairs against the root user.
		AdditionalUsers []UserConfig `yaml:"additionalUsers"`

		// ArgoCD specific details.
		ArgoCD ArgoCDConfig `yaml:"argoCD" validate:"required"`
	}

	// ACMEDNS01Config selects and scopes the ClusterIssuer's DNS-01
	// solver. Only Cloudflare is wired today (the chart's solver list
	// also knows route53; extend Provider's oneof when kubeaid-cli
	// grows the matching credential plumbing).
	ACMEDNS01Config struct {
		Provider string `yaml:"provider" default:"cloudflare" validate:"oneof=cloudflare"`

		// DNSZones limits which zones this solver answers challenges
		// for (cert-manager's selector.dnsZones). Empty matches every
		// DNS-01 order — fine when this is the only solver.
		DNSZones []string `yaml:"dnsZones"`
	}

	ArgoCDConfig struct {
		DeployKeys DeployKeysConfig `yaml:"deployKeys" validate:"required"`
	}

	DeployKeysConfig struct {
		Kubeaid       *SSHKeyPairConfig `yaml:"kubeaid"`
		KubeaidConfig SSHKeyPairConfig  `yaml:"kubeaidConfig" validate:"required"`
	}

	// REFER : https://github.com/kubernetes-sigs/cluster-api/blob/main/controlplane/kubeadm/config/crd/bases/controlplane.cluster.x-k8s.io_kubeadmcontrolplanes.yaml.
	//
	// NOTE : Generally, refer to the KubeadmControlPlane CRD instead of the corresponding GoLang
	//        source types linked below. There are some configuration options which appear in the
	//        corresponding GoLang source type, but not in the CRD. If you set those fields, then
	//        they get removed by the Kubeadm control-plane provider. This causes the capi-cluster
	//        ArgoCD App to always be in an OutOfSync state, resulting to KubeAid CLI not making any
	//        progress!
	APIServerConfig struct {
		ExtraArgs    map[string]string     `yaml:"extraArgs"    default:"{}"`
		ExtraVolumes []HostPathMountConfig `yaml:"extraVolumes" default:"[]"`
		Files        []FileConfig          `yaml:"files"        default:"[]"`
	}

	// KeycloakConfig declares the Keycloak instance a VPN cluster hosts as
	// NetBird's SSO IdP. The parser derives the Realm from DNS when unset
	// and validates the combination against cluster.type. The admin
	// password is generated by kubeaid-cli at bootstrap and never lives in
	// this struct or in secrets.yaml; only Mode/DNS/Realm are user-facing.
	KeycloakConfig struct {
		// Mode is "managed" (kubeaid-cli installs Keycloak via the
		// keycloakx Helm chart on this cluster — VPN clusters only)
		// or "external" (Keycloak is already running elsewhere;
		// supply DNS only). Workload clusters must use external.
		Mode string `yaml:"mode" validate:"oneof=managed external"`

		// DNS is the public hostname Keycloak is reachable at, e.g.
		// "keycloak.vpn.acme.com". Required. Used to derive the OIDC
		// issuer URL the apiserver and kubelogin trust, and (when
		// Realm is unset) to default the realm name.
		DNS string `yaml:"dns" validate:"required"`

		// Realm is the Keycloak realm name. Optional — when empty,
		// kubeaid-cli derives it from DNS via
		// `golang.org/x/net/publicsuffix.EffectiveTLDPlusOne` and the
		// first dot-separated segment of the result. Examples:
		//   keycloak.vpn.acme.com  → "acme"
		//   keycloak.foo.co.uk     → "foo"
		// Set this explicitly to override the derivation.
		Realm string `yaml:"realm"`
	}

	// NetBirdConfig describes this cluster's relationship to the NetBird
	// mesh. It is valid for both cluster.type=vpn (which hosts NetBird
	// Mgmt) and cluster.type=workload (which only joins the mesh):
	// dns/stun/turn are meaningful only on the VPN host, while dnsZone
	// applies to any cluster on the mesh. cluster.type is the gate.
	NetBirdConfig struct {
		// DNS is the public hostname NetBird Management is reachable at,
		// e.g. "netbird.vpn.acme.com". Required only for cluster.type=vpn
		// (enforced in parser/keycloak.go); unused on workload clusters.
		DNS string `yaml:"dns" validate:"omitempty,fqdn"`

		// DNSZone is the mesh DNS domain peers resolve under — NetBird
		// Mgmt's --dns-domain, e.g. "mesh.acme.com". Operator-supplied, no
		// default. Required for cluster.type=vpn and for workload clusters
		// that join a mesh; absent on workload clusters that don't. Used to
		// create the DNS zone on NetBird, to drive --dns-domain on VPN
		// clusters, and to add the kubernetes.<dnsZone> apiserver cert SAN.
		DNSZone string `yaml:"dnsZone" validate:"omitempty,fqdn|hostname_rfc1123"`

		// StunDNS is the public hostname Coturn answers STUN queries
		// on, e.g. "stun.vpn.acme.com". Optional: kubeaid-cli derives
		// it as "stun.<base>" where base is DNS with the leading
		// "netbird." stripped (so netbird.vpn.acme.com → stun.vpn.acme.com).
		// Override only when STUN is exposed on a non-standard FQDN.
		StunDNS string `yaml:"stunDNS"`

		// TurnDNS is the public hostname Coturn answers TURN queries
		// on, e.g. "turn.vpn.acme.com". Optional: derived as
		// "turn.<base>" by the same logic as StunDNS.
		TurnDNS string `yaml:"turnDNS"`

		// TurnUser is the static username Coturn / NetBird Mgmt agree
		// on for TURN authentication. The matching password is
		// generated and persisted in the Secret. Optional, defaults
		// to "netbird".
		TurnUser string `yaml:"turnUser" default:"netbird"`

		// ClusterProxy configures the netbird-operator's kube-apiserver
		// proxy (operator >= 0.7.0): a mesh peer that proxies kubectl to
		// the in-cluster apiserver, impersonating the caller's NetBird
		// identity. Omit the block to leave it disabled.
		ClusterProxy *NetBirdClusterProxyConfig `yaml:"clusterProxy"`

		// Groups are extra NetBird groups this cluster OWNS (chart: groups), beyond
		// the derived k8s-<cluster> and k8s-<cluster>-access. Declare a group from ONE
		// cluster only — a duplicate wedges that operator on HTTP 409.
		Groups []string `yaml:"groups" validate:"omitempty,dive,required"`
	}

	// NetBirdClusterProxyConfig configures the netbird-operator kube-apiserver
	// proxy (netbird-operator.clusterProxy in the chart values). The proxy
	// registers under cluster.name (netbird kubernetes write-kubeconfig
	// <cluster.name>).
	NetBirdClusterProxyConfig struct {
		// Enabled toggles the cluster proxy.
		Enabled bool `yaml:"enabled"`

		// RBAC binds NetBird groups to cluster roles via the proxy's
		// identity impersonation.
		RBAC []NetBirdClusterProxyRBACConfig `yaml:"rbac" validate:"omitempty,dive"`
	}

	// NetBirdClusterProxyRBACConfig binds one NetBird group to one
	// ClusterRole through the cluster proxy.
	NetBirdClusterProxyRBACConfig struct {
		Group       string `yaml:"group"       validate:"required"`
		ClusterRole string `yaml:"clusterRole" validate:"required"`
	}

	// REFER : "sigs.k8s.io/cluster-api/bootstrap/kubeadm/api/v1beta1".HostPathMount
	HostPathMountConfig struct {
		Name      string              `yaml:"name"      validate:"notblank"`
		HostPath  string              `yaml:"hostPath"  validate:"notblank"`
		MountPath string              `yaml:"mountPath" validate:"notblank"`
		PathType  coreV1.HostPathType `yaml:"pathType"  validate:"required"`

		// Whether the mount should be read-only.
		ReadOnly bool `yaml:"readOnly" default:"true"`
	}

	// REFER : "sigs.k8s.io/cluster-api/bootstrap/kubeadm/api/v1beta1".File.
	FileConfig struct {
		Path    string `yaml:"path"    validate:"notblank"`
		Content string `yaml:"content" validate:"notblank"`
	}

	UserConfig struct {
		Name         string `yaml:"name"         validate:"required"`
		SSHPublicKey string `yaml:"sshPublicKey" validate:"required"`
	}

	NodeGroup struct {
		// Nodegroup name.
		Name string `yaml:"name" validate:"notblank"`

		// Labels that you want to be propagated to each node in the nodegroup.
		//
		// Each label should meet one of the following criterias to propagate to each of the nodes :
		//
		//   1. Has node-role.kubernetes.io as prefix.
		//   2. Belongs to node-restriction.kubernetes.io domain.
		//   3. Belongs to node.cluster.x-k8s.io domain.
		//
		// REFER : https://cluster-api.sigs.k8s.io/developer/architecture/controllers/metadata-propagation#machine.
		Labels map[string]string `yaml:"labels" default:"[]"`

		// Taints that you want to be propagated to each node in the nodegroup.
		Taints []*coreV1.Taint `yaml:"taints" default:"[]"`
	}

	AutoScalableNodeGroup struct {
		NodeGroup `yaml:",inline"`

		CPU    uint32 `validate:"required"`
		Memory uint32 `validate:"required"`

		// Minimum number of replicas in the nodegroup.
		MinSize uint `yaml:"minSize" validate:"required"`

		// Maximum number of replicas in the nodegroup.
		Maxsize uint `yaml:"maxSize" validate:"required"`
	}

	CloudConfig struct {
		AWS       *AWSConfig       `yaml:"aws"`
		Azure     *AzureConfig     `yaml:"azure"`
		Hetzner   *HetznerConfig   `yaml:"hetzner"`
		BareMetal *BareMetalConfig `yaml:"bare-metal"`
		Local     *LocalConfig     `yaml:"local"`

		DisasterRecovery *DisasterRecoveryConfig `yaml:"disasterRecovery"`
	}

	DisasterRecoveryConfig struct {
		VeleroBackupsBucketName        string `yaml:"veleroBackupsBucketName"`
		SealedSecretsBackupsBucketName string `yaml:"sealedSecretsBackupsBucketName"`
	}

	SSHKeyPairConfig struct {
		// PrivateKeyFilePath is the on-disk SSH private key
		// kubeaid-cli reads to derive PublicKey + Fingerprint and
		// (for cloud-side SSH connections like the Hetzner NAT
		// gateway setup) to authenticate the SSH session. Required
		// when UseSSHAgent is false; ignored when UseSSHAgent is
		// true (the agent owns the private key — yubikey case —
		// so there's nothing on disk to point at). Cross-field
		// validation in pkg/config/parser/validate.go enforces
		// "exactly one is set".
		PrivateKeyFilePath string `yaml:"privateKeyFilePath"`

		// UseSSHAgent flips the SSH key sourcing from "read a file
		// from PrivateKeyFilePath" to "dial $SSH_AUTH_SOCK and ask
		// the agent for its loaded identities". The first identity
		// supplies PublicKey + Fingerprint; the SSH client (kubeone)
		// signs through the agent socket so yubikey-resident
		// private keys never need to be exported.
		UseSSHAgent bool `yaml:"useSSHAgent"`

		//nolint:gosec // This struct intentionally stores hydrated SSH key material.
		PrivateKey,

		PublicKey,
		Fingerprint string
	}

	KubePrometheusConfig struct {
		Version    string `yaml:"version"`
		GrafanaURL string `yaml:"grafanaURL"`
	}

	ObmondoConfig struct {
		CustomerID string `yaml:"customerID"`
		Monitoring bool   `yaml:"monitoring"`

		// Path to the mTLS client cert issued by Obmondo. Required when
		// Monitoring is true — kubeaid-agent uses it to authenticate to the
		// Obmondo API, and kube-prometheus's Alertmanager uses it to push
		// alerts to Obmondo's alert-receiver endpoint.
		CertPath string `yaml:"certPath"`

		// Path to the private key paired with CertPath. Required when
		// Monitoring is true.
		KeyPath string `yaml:"keyPath"`
	}
)

// AWS specific.
type (
	AWSConfig struct {
		Region string `yaml:"region" validate:"notblank"`

		SSHKeyName     string                     `yaml:"sshKeyName"     validate:"notblank"`
		VPCID          *string                    `yaml:"vpcID"`
		BastionEnabled bool                       `yaml:"bastionEnabled"                     default:"True"`
		ControlPlane   AWSControlPlane            `yaml:"controlPlane"   validate:"required"`
		NodeGroups     []AWSAutoScalableNodeGroup `yaml:"nodeGroups"`
	}

	AWSControlPlane struct {
		LoadBalancerScheme string    `yaml:"loadBalancerScheme" default:"internet-facing" validate:"notblank"`
		Replicas           uint32    `yaml:"replicas"                                     validate:"required"`
		InstanceType       string    `yaml:"instanceType"                                 validate:"notblank"`
		AMI                AMIConfig `yaml:"ami"                                          validate:"required"`
	}

	AWSAutoScalableNodeGroup struct {
		AutoScalableNodeGroup `yaml:",inline"`

		AMI            AMIConfig `yaml:"ami"            validate:"required"`
		InstanceType   string    `yaml:"instanceType"   validate:"notblank"`
		RootVolumeSize uint32    `yaml:"rootVolumeSize" validate:"required"`
		SSHKeyName     string    `yaml:"sshKeyName"     validate:"notblank"`
	}

	AMIConfig struct {
		ID string `yaml:"id" validate:"notblank"`
	}
)

// Azure specific.
type (
	AzureConfig struct {
		TenantID       string         `yaml:"tenantID"       validate:"notblank"`
		SubscriptionID string         `yaml:"subscriptionID" validate:"notblank"`
		AADApplication AADApplication `yaml:"aadApplication" validate:"required"`
		Location       string         `yaml:"location"       validate:"notblank"`

		StorageAccount string `yaml:"storageAccount" validate:"notblank"`

		WorkloadIdentity WorkloadIdentity `yaml:"workloadIdentity" validate:"required"`

		SSHPublicKey string `yaml:"sshPublicKey" validate:"notblank"`

		CanonicalUbuntuImage CanonicalUbuntuImage `yaml:"canonicalUbuntuImage" validate:"required"`

		ControlPlane AzureControlPlane            `yaml:"controlPlane" validate:"required"`
		NodeGroups   []AzureAutoScalableNodeGroup `yaml:"nodeGroups"`
	}

	AADApplication struct {
		PrincipalID string `yaml:"principalID" validate:"notblank"`
	}

	WorkloadIdentity struct {
		OpenIDProviderSSHKeyPair OpenIDProviderSSHKeyPairConfig `yaml:"openIDProviderSSHKeyPair" validate:"required"`
	}

	OpenIDProviderSSHKeyPairConfig struct {
		SSHKeyPairConfig  `       yaml:",inline"`
		PublicKeyFilePath string `yaml:"publicKeyFilePath" validate:"notblank"`
	}

	CanonicalUbuntuImage struct {
		Offer string `yaml:"offer" validate:"notblank"`
		SKU   string `yaml:"sku"   validate:"notblank"`
	}

	AzureControlPlane struct {
		LoadBalancerType string `yaml:"loadBalancerType" validate:"notblank"        default:"Public"`
		DiskSizeGB       uint32 `yaml:"diskSizeGB"       validate:"required,gt=100"`
		VMSize           string `yaml:"vmSize"           validate:"notblank"`
		Replicas         uint32 `yaml:"replicas"         validate:"required,gt=0"`
	}

	AzureAutoScalableNodeGroup struct {
		AutoScalableNodeGroup `yaml:",inline"`

		VMSize     string `yaml:"vmSize"     validate:"notblank"`
		DiskSizeGB uint32 `yaml:"diskSizeGB" validate:"required"`
	}
)

// Hetzner specific.
type (
	HetznerConfig struct {
		/*
			The Hetzner mode to use :

			  (1) hcloud : Both the control-plane and the nodegroups will be in HCloud.

			  (2) bare-metal : Both the control-plane and the nodegroups will be in Hetzner Bare Metal.

			  (3) hybrid : The control-plane will be in HCloud, and each node-group can be either in
			               HCloud or Hetzner Bare Metal.
		*/
		Mode string `yaml:"mode" default:"hcloud" validate:"notblank,oneof=bare-metal hcloud hybrid"`

		// Details about the VPN cluster you have in HCloud.
		HCloudVPNCluster *HCloudVPNClusterConfig `yaml:"hcloudVPNCluster"`

		// Details about the SSH keypair which will be used to SSH into the HCloud or / and Hetzner
		// Bare Metal server.
		// KubeAid CLI will create the corresponding HCloud or / and Hetzner Bare Metal SSH keypairs,
		// if it / they doesn't already exist.
		SSHKeyPair HetznerSSHKeyPair `yaml:"sshKeyPair" validate:"required"`

		// HCloud specific details.
		HCloud *HCloudConfig `yaml:"hcloud"`

		// Hetzner bare-metal specific details.
		BareMetal *HetznerBareMetalConfig `yaml:"bareMetal"`

		// Control-plane specific details.
		ControlPlane HetznerControlPlane `yaml:"controlPlane" validate:"required"`

		// Details about the node-groups.
		NodeGroups HetznerNodeGroups `yaml:"nodeGroups"`
	}

	HCloudVPNClusterConfig struct {
		Name string `yaml:"name" validate:"notblank"`
	}

	HetznerSSHKeyPair struct {
		Name             string `yaml:"name"    validate:"notblank"`
		SSHKeyPairConfig `       yaml:",inline"`
	}

	HCloudConfig struct {
		Zone      string `yaml:"zone"      validate:"notblank"`
		ImageName string `yaml:"imageName" validate:"notblank" default:"ubuntu-26.04"`

		// NATGatewayServerType is the HCloud server type for the NAT gateway
		// that fronts the private network during bootstrap. cpx22 is a small,
		// cost-optimised x86 box — ample for NAT. Override it if cpx22 is out
		// of stock / not offered in your locations, or you need more throughput
		// (`hcloud server-type list` shows what's available).
		NATGatewayServerType string `yaml:"natGatewayServerType" validate:"notblank" default:"cpx22"`

		// Hetzner Network specific details.
		HetznerNetwork HetznerNetworkConfig `yaml:"hetznerNetwork" validate:"required"`
	}

	HetznerNetworkConfig struct {
		CIDR                    string `yaml:"cidr"                    validate:"cidrv4"`
		HCloudServersSubnetCIDR string `yaml:"hcloudServersSubnetCIDR" validate:"cidrv4"`
	}

	HetznerBareMetalConfig struct {
		WipeDisks    bool               `yaml:"wipeDisks"    default:"false"`
		InstallImage InstallImageConfig `yaml:"installImage"`

		// Firewall configures the Cilium host-firewall policy (CiliumClusterwideNetworkPolicy)
		// that locks down each bare-metal node's public NIC. Enabled controls whether
		// kubeaid-cli renders the policy at all; AllowSSHFrom feeds the per-CIDR SSH ingress
		// rule. See docs/hetzner-bare-metal-network-surface.md.
		Firewall FirewallConfig `yaml:"firewall"`

		// ZFS specific configuration.
		// Every node runs a ZFS pool, named primary. We carve out storage for container images, pod
		// logs and pod ephemeral volumes from that ZFS pool, as required.
		// The ZFS pool has RAIDZ-1 enabled, which means it can survive single disk failure.
		ZFS ZFSConfig `yaml:"zfs" validate:"required"`

		// Details about the VSwitch which'll be used to connect the Hetzner Bare Metal servers with
		// the Hetzner Network.
		VSwitch *VSwitchConfig `yaml:"vSwitch"`
	}

	InstallImageConfig struct {
		ImagePath string    `yaml:"imagePath" default:"/root/.oldroot/nfs/images/Ubuntu-2604-resolute-amd64-base.tar.zst" validate:"notblank"`
		VG0       VG0Config `yaml:"vg0"`
	}

	VG0Config struct {
		Size           int `yaml:"size"           validate:"notblank" default:"80"`
		RootVolumeSize int `yaml:"rootVolumeSize" validate:"notblank" default:"50"`
	}

	VSwitchConfig struct {
		VLANID int    `yaml:"vlanID"`
		Name   string `yaml:"name"   validate:"notblank"`

		// SubnetCIDRBlock is the vSwitch subnet attached to the Hetzner Network.
		// The IP written here doubles as the subnet's gateway (net.ParseCIDR's
		// first return), so "10.0.1.0/24" yields gateway 10.0.1.0 — write the IP
		// you want as the gateway, not just any address in the range.
		SubnetCIDRBlock string `yaml:"subnetCIDRBlock" validate:"cidrv4"`
	}

	// FirewallConfig drives the Cilium host-firewall policy rendered by kubeaid-cli
	// for Hetzner bare-metal clusters. The resulting CiliumClusterwideNetworkPolicy
	// selects every node and locks down the public NIC via eBPF host-endpoint rules.
	// See docs/hetzner-bare-metal-network-surface.md.
	FirewallConfig struct {
		// Enabled gates whether kubeaid-cli renders the Cilium host-firewall
		// CiliumClusterwideNetworkPolicy at all. Defaults to true; set false to
		// opt out — e.g. a separate upstream L3 firewall appliance already fronts
		// the cluster. A pointer so an explicit "enabled: false" is distinguishable
		// from unset and honoured.
		Enabled *bool `yaml:"enabled"`

		// AllowSSHFrom restricts inbound SSH (22/tcp) on every bare-metal node to
		// these sources. Rendered as a fromCIDR rule in the CCNP. Empty (the
		// default) allows SSH from anywhere — matching the bare-metal posture where
		// nodes are not NetBird peers and have no mesh fallback path. Each entry is
		// an IPv4 address or CIDR (e.g. "203.0.113.4" or "203.0.113.0/24"); a bare
		// address is treated as /32.
		AllowSSHFrom []string `yaml:"allowSshFrom" validate:"omitempty,dive,ipv4|cidrv4"`

		// AllowPublic is a legacy field from the (removed) Hetzner Robot firewall.
		// It is parsed and validated but NOT rendered into the Cilium host-firewall
		// policy — parser.validateHetznerConfig only logs a warning when it is set.
		// The policy's world-facing ports come from hostNetworkPolicy.publicPorts
		// (chart default [80, 443]); 6443 is never world-public — it is a separate
		// rule restricted to hostNetworkPolicy.apiserverSourceCIDRs (the node IPs).
		// To open extra ports to the world, add them to hostNetworkPolicy.publicPorts
		// in the cilium chart values overlay, not here.
		AllowPublic []FirewallPort `yaml:"allowPublic" validate:"omitempty,dive"`
	}

	// FirewallPort is one {port, protocol} entry in FirewallConfig.AllowPublic.
	FirewallPort struct {
		// Port is a single port ("25") or an inclusive range ("30000-32767").
		Port string `yaml:"port" validate:"notblank"`

		// Protocol is "tcp", "udp", or omitted for any protocol.
		Protocol string `yaml:"protocol" validate:"omitempty,oneof=tcp udp"`
	}

	HetznerControlPlane struct {
		HCloud    *HCloudControlPlane           `yaml:"hcloud"`
		BareMetal *HetznerBareMetalControlPlane `yaml:"bareMetal"`

		// Regions is the list of Hetzner regions (lower-case IDs: "fsn1", "hel1", "ash", ...)
		// the CAPH chart constrains control-plane placement to. At least one is required.
		Regions []string `yaml:"regions" validate:"required,min=1,dive,notblank"`

		// ExtraCertSANs are additional DNS names added to the apiserver's
		// TLS cert SAN list, on every Hetzner mode (hcloud, bare-metal,
		// hybrid). The chart merges these with endpoint.host into kubeadm's
		// apiServer.certSANs. Use for any additional hostnames clients reach
		// the apiserver under.
		ExtraCertSANs []string `yaml:"extraCertSANs,omitempty" validate:"omitempty,dive,fqdn|hostname_rfc1123"`
	}

	HCloudControlPlane struct {
		MachineType  string                         `yaml:"machineType"  validate:"notblank"`
		Replicas     uint                           `yaml:"replicas"     validate:"notblank"`
		LoadBalancer HCloudControlPlaneLoadBalancer `yaml:"loadBalancer" validate:"required"`
	}

	HetznerBareMetalControlPlane struct {
		Endpoint       HetznerBareMetalControlPlaneEndpoint `yaml:"endpoint"       validate:"required"`
		BareMetalHosts []*HetznerBareMetalHost              `yaml:"bareMetalHosts" validate:"required,gt=0"`

		// ZFS pool size on each control-plane node. See ZFSConfig.Size for sizing rules.
		ZFS ZFSConfig `yaml:"zfs" validate:"required"`

		StoragePlan storageplan.StoragePlan `yaml:"-"`
	}

	HetznerBareMetalControlPlaneEndpoint struct {
		IsFailoverIP bool   `yaml:"isFailoverIP"`
		Host         string `yaml:"host"         validate:"ip"`
	}

	HCloudControlPlaneLoadBalancer struct {
		Enabled bool   `yaml:"enabled" validate:"required"`
		Region  string `yaml:"region"  validate:"notblank"`

		// Endpoint is the FQDN clients use to reach kube-apiserver
		// (CAPI's controlPlaneEndpoint.host, kubeadm cert SAN,
		// kubeconfig server URL). Optional: when omitted, the LB
		// private IP is used as the control-plane endpoint directly
		// (no public interface, no DNS wait). When set, the LB gets
		// a public interface during bootstrap and kubeaid-cli waits
		// for the operator's DNS A-record to land before continuing.
		// DNS resolution is the operator's responsibility.
		Endpoint string `yaml:"endpoint" validate:"omitempty,fqdn"`
	}

	// Details about node-groups in Hetzner.
	HetznerNodeGroups struct {
		// Details about node-groups in HCloud.
		HCloud []HCloudAutoScalableNodeGroup `yaml:"hcloud"`

		// Details about node-groups in Hetzner Bare Metal.
		BareMetal []*HetznerBareMetalNodeGroup `yaml:"bareMetal"`
	}

	// Details about (autoscalable) node-groups in HCloud.
	HCloudAutoScalableNodeGroup struct {
		AutoScalableNodeGroup `yaml:",inline"`

		// HCloud machine type.
		// You can browse all available HCloud machine types here : https://hetzner.com/cloud.
		MachineType string `yaml:"machineType" validate:"notblank"`

		// The root volume size for each HCloud machine.
		RootVolumeSize uint32 `validate:"required"`
	}

	HetznerBareMetalNodeGroup struct {
		NodeGroup `yaml:",inline"`

		BareMetalHosts []*HetznerBareMetalHost `yaml:"bareMetalHosts" validate:"required,gt=0"`

		// ZFS specific configuration.
		// Every node runs a ZFS pool, named primary. We carve out storage for container images, pod
		// logs and pod ephemeral volumes from that ZFS pool, as required.
		// The ZFS pool has RAIDZ-1 enabled, which means it can survive single disk failure.
		ZFS ZFSConfig `yaml:"zfs" validate:"required"`

		StoragePlan storageplan.StoragePlan `yaml:"-"`
	}

	HetznerBareMetalHost struct {
		ServerID  string `yaml:"serverID"  validate:"notblank"`
		PrivateIP string `yaml:"privateIP" validate:"ipv4"`
		WWNs      []string
	}

	ZFSConfig struct {
		// ZFS pool size (in GB), on each node in the corresponding node-group.
		// Must be >= 200 GB : reserving 100 GB for container images, 50 GB for pod logs and 50 GB for
		// pod ephemeral volumes.
		// On top of that, if you want x GB of node-local storage for your workloads (like Redis),
		// the ZFS pool size will be (200 + 2x) GB, keeping in mind that RAIDZ-1 is enabled.
		Size int `yaml:"size" validate:"required,gt=200" default:"220"`
	}
)

// Bare Metal specific.
type (
	BareMetalConfig struct {
		SSH BareMetalSSHConfig `yaml:"ssh"`

		// Kubelet tuning applied to every host (control-plane and workers).
		Kubelet *BareMetalKubeletConfig `yaml:"kubelet"`

		ControlPlane BareMetalControlPlane `yaml:"controlPlane" validate:"required"`
		NodeGroups   []BareMetalNodeGroup  `yaml:"nodeGroups"`
	}

	// BareMetalKubeletConfig mirrors KubeOne's per-host KubeletConfig.
	// REFER : https://docs.kubermatic.com/kubeone/v1.13/references/kubeone-cluster-v1beta2/#kubeletconfig
	BareMetalKubeletConfig struct {
		SystemReserved map[string]string `yaml:"systemReserved,omitempty"`
		KubeReserved   map[string]string `yaml:"kubeReserved,omitempty"`
		EvictionHard   map[string]string `yaml:"evictionHard,omitempty"`
		MaxPods        *int32            `yaml:"maxPods,omitempty"`
	}

	BareMetalSSHConfig struct {
		Port              uint `yaml:"port"    validate:"required" default:"22"`
		*SSHKeyPairConfig `     yaml:",inline"`
	}

	BareMetalControlPlane struct {
		Endpoint BareMetalControlPlaneEndpoint `yaml:"endpoint" validate:"required"`
		Hosts    []*BareMetalHost              `yaml:"hosts"    validate:"required"`
	}

	BareMetalControlPlaneEndpoint struct {
		Host string `yaml:"host" validate:"notblank"`
		Port uint   `yaml:"port" validate:"required" default:"6443"`
	}

	BareMetalNodeGroup struct {
		NodeGroup `yaml:",inline"`

		Hosts []*BareMetalHost `yaml:"hosts" validate:"required"`
	}

	BareMetalHost struct {
		PublicAddress  *string `yaml:"publicAddress"  validate:"notblank"`
		PrivateAddress *string `yaml:"privateAddress" validate:"notblank"`

		SSH *BareMetalSSHConfig `yaml:"ssh"`
	}
)

// Local specific.
type (
	LocalConfig struct{}
)
