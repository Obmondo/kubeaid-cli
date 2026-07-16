# Configuration Reference
- [AADApplication](#aadapplication)
- [ACMECredentials](#acmecredentials)
- [ACMEDNS01Config](#acmedns01config)
- [AMIConfig](#amiconfig)
- [APIServerConfig](#apiserverconfig)
- [AWSAutoScalableNodeGroup](#awsautoscalablenodegroup)
- [AWSConfig](#awsconfig)
- [AWSControlPlane](#awscontrolplane)
- [AWSCredentials](#awscredentials)
- [ArgoCDConfig](#argocdconfig)
- [AutoScalableNodeGroup](#autoscalablenodegroup)
- [AzureAutoScalableNodeGroup](#azureautoscalablenodegroup)
- [AzureConfig](#azureconfig)
- [AzureControlPlane](#azurecontrolplane)
- [AzureCredentials](#azurecredentials)
- [BareMetalConfig](#baremetalconfig)
- [BareMetalControlPlane](#baremetalcontrolplane)
- [BareMetalControlPlaneEndpoint](#baremetalcontrolplaneendpoint)
- [BareMetalHost](#baremetalhost)
- [BareMetalKubeletConfig](#baremetalkubeletconfig)
- [BareMetalNodeGroup](#baremetalnodegroup)
- [BareMetalSSHConfig](#baremetalsshconfig)
- [CanonicalUbuntuImage](#canonicalubuntuimage)
- [CloudConfig](#cloudconfig)
- [ClusterConfig](#clusterconfig)
- [DeployKeysConfig](#deploykeysconfig)
- [DisasterRecoveryConfig](#disasterrecoveryconfig)
- [FileConfig](#fileconfig)
- [FirewallConfig](#firewallconfig)
- [FirewallPort](#firewallport)
- [ForksConfig](#forksconfig)
- [GeneralConfig](#generalconfig)
- [GitConfig](#gitconfig)
- [HCloudAutoScalableNodeGroup](#hcloudautoscalablenodegroup)
- [HCloudConfig](#hcloudconfig)
- [HCloudControlPlane](#hcloudcontrolplane)
- [HCloudControlPlaneLoadBalancer](#hcloudcontrolplaneloadbalancer)
- [HCloudVPNClusterConfig](#hcloudvpnclusterconfig)
- [HetznerBareMetalConfig](#hetznerbaremetalconfig)
- [HetznerBareMetalControlPlane](#hetznerbaremetalcontrolplane)
- [HetznerBareMetalControlPlaneEndpoint](#hetznerbaremetalcontrolplaneendpoint)
- [HetznerBareMetalHost](#hetznerbaremetalhost)
- [HetznerBareMetalNodeGroup](#hetznerbaremetalnodegroup)
- [HetznerConfig](#hetznerconfig)
- [HetznerControlPlane](#hetznercontrolplane)
- [HetznerCredentials](#hetznercredentials)
- [HetznerNetworkConfig](#hetznernetworkconfig)
- [HetznerNodeGroups](#hetznernodegroups)
- [HetznerRobotCredentials](#hetznerrobotcredentials)
- [HetznerSSHKeyPair](#hetznersshkeypair)
- [HostPathMountConfig](#hostpathmountconfig)
- [InstallImageConfig](#installimageconfig)
- [KeycloakConfig](#keycloakconfig)
- [KeycloakCredentials](#keycloakcredentials)
- [KubeAidForkConfig](#kubeaidforkconfig)
- [KubePrometheusConfig](#kubeprometheusconfig)
- [KubeaidConfigForkConfig](#kubeaidconfigforkconfig)
- [KubeaidStoragectlConfig](#kubeaidstoragectlconfig)
- [LocalConfig](#localconfig)
- [NetBirdClusterProxyConfig](#netbirdclusterproxyconfig)
- [NetBirdClusterProxyRBACConfig](#netbirdclusterproxyrbacconfig)
- [NetBirdConfig](#netbirdconfig)
- [NetBirdCredentials](#netbirdcredentials)
- [NodeGroup](#nodegroup)
- [ObmondoConfig](#obmondoconfig)
- [OpenIDProviderSSHKeyPairConfig](#openidprovidersshkeypairconfig)
- [SSHKeyPairConfig](#sshkeypairconfig)
- [SecretsConfig](#secretsconfig)
- [UserConfig](#userconfig)
- [VG0Config](#vg0config)
- [VSwitchConfig](#vswitchconfig)
- [WorkloadIdentity](#workloadidentity)
- [ZFSConfig](#zfsconfig)

## AADApplication

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| principalID | `string` |  |  |

## ACMECredentials

<p>ACMECredentials carries the DNS-provider secrets the cert-manager
ClusterIssuer's DNS-01 solver authenticates with. Only needed when
cluster.acmeDNS01 is set.</p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| cloudflareApiToken | `string` |  | CloudflareAPIToken is a Cloudflare API token with Zone:Read +<br>DNS:Edit on the zones the solver manages (the TXT challenge<br>records). Sealed into the cert-manager/cloudflare-api-token<br>Secret the ClusterIssuer references.<br> |

## ACMEDNS01Config

<p>ACMEDNS01Config selects and scopes the ClusterIssuer's DNS-01
solver. Only Cloudflare is wired today (the chart's solver list
also knows route53; extend Provider's oneof when kubeaid-cli
grows the matching credential plumbing).</p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| provider | `string` | cloudflare |  |
| dnsZones | []`string` |  | DNSZones limits which zones this solver answers challenges<br>for (cert-manager's selector.dnsZones). Empty matches every<br>DNS-01 order — fine when this is the only solver.<br> |

## AMIConfig

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| id | `string` |  |  |

## APIServerConfig

<p>REFER : https://github.com/kubernetes-sigs/cluster-api/blob/main/controlplane/kubeadm/config/crd/bases/controlplane.cluster.x-k8s.io_kubeadmcontrolplanes.yaml.

NOTE : Generally, refer to the KubeadmControlPlane CRD instead of the corresponding GoLang
       source types linked below. There are some configuration options which appear in the
       corresponding GoLang source type, but not in the CRD. If you set those fields, then
       they get removed by the Kubeadm control-plane provider. This causes the capi-cluster
       ArgoCD App to always be in an OutOfSync state, resulting to KubeAid CLI not making any
       progress!</p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| extraArgs | `map[string]string` | {} |  |
| extraVolumes | [][`HostPathMountConfig`](#hostpathmountconfig) | [] |  |
| files | [][`FileConfig`](#fileconfig) | [] |  |

## AWSAutoScalableNodeGroup

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| ami | [`AMIConfig`](#amiconfig) |  |  |
| instanceType | `string` |  |  |
| rootVolumeSize | `uint32` |  |  |
| sshKeyName | `string` |  |  |
| name | `string` |  | Nodegroup name.<br> |
| labels | `map[string]string` | [] | Labels that you want to be propagated to each node in the nodegroup.<br><br>Each label should meet one of the following criterias to propagate to each of the nodes :<br><br>  1. Has node-role.kubernetes.io as prefix.<br>  2. Belongs to node-restriction.kubernetes.io domain.<br>  3. Belongs to node.cluster.x-k8s.io domain.<br><br>REFER : https://cluster-api.sigs.k8s.io/developer/architecture/controllers/metadata-propagation#machine.<br> |
| taints | []`k8s.io/api/core/v1.Taint` | [] | Taints that you want to be propagated to each node in the nodegroup.<br> |
| minSize | `uint` |  | Minimum number of replicas in the nodegroup.<br> |
| maxSize | `uint` |  | Maximum number of replicas in the nodegroup.<br> |

## AWSConfig

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| region | `string` |  |  |
| sshKeyName | `string` |  |  |
| vpcID | `string` |  |  |
| bastionEnabled | `bool` | True |  |
| controlPlane | [`AWSControlPlane`](#awscontrolplane) |  |  |
| nodeGroups | [][`AWSAutoScalableNodeGroup`](#awsautoscalablenodegroup) |  |  |

## AWSControlPlane

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| loadBalancerScheme | `string` | internet-facing |  |
| replicas | `uint32` |  |  |
| instanceType | `string` |  |  |
| ami | [`AMIConfig`](#amiconfig) |  |  |

## AWSCredentials

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| accessKeyID | `string` |  |  |
| secretAccessKey | `string` |  |  |
| sessionToken | `string` |  |  |

## ArgoCDConfig

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| deployKeys | [`DeployKeysConfig`](#deploykeysconfig) |  |  |

## AutoScalableNodeGroup

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| minSize | `uint` |  | Minimum number of replicas in the nodegroup.<br> |
| maxSize | `uint` |  | Maximum number of replicas in the nodegroup.<br> |
| name | `string` |  | Nodegroup name.<br> |
| labels | `map[string]string` | [] | Labels that you want to be propagated to each node in the nodegroup.<br><br>Each label should meet one of the following criterias to propagate to each of the nodes :<br><br>  1. Has node-role.kubernetes.io as prefix.<br>  2. Belongs to node-restriction.kubernetes.io domain.<br>  3. Belongs to node.cluster.x-k8s.io domain.<br><br>REFER : https://cluster-api.sigs.k8s.io/developer/architecture/controllers/metadata-propagation#machine.<br> |
| taints | []`k8s.io/api/core/v1.Taint` | [] | Taints that you want to be propagated to each node in the nodegroup.<br> |

## AzureAutoScalableNodeGroup

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| vmSize | `string` |  |  |
| diskSizeGB | `uint32` |  |  |
| name | `string` |  | Nodegroup name.<br> |
| labels | `map[string]string` | [] | Labels that you want to be propagated to each node in the nodegroup.<br><br>Each label should meet one of the following criterias to propagate to each of the nodes :<br><br>  1. Has node-role.kubernetes.io as prefix.<br>  2. Belongs to node-restriction.kubernetes.io domain.<br>  3. Belongs to node.cluster.x-k8s.io domain.<br><br>REFER : https://cluster-api.sigs.k8s.io/developer/architecture/controllers/metadata-propagation#machine.<br> |
| taints | []`k8s.io/api/core/v1.Taint` | [] | Taints that you want to be propagated to each node in the nodegroup.<br> |
| minSize | `uint` |  | Minimum number of replicas in the nodegroup.<br> |
| maxSize | `uint` |  | Maximum number of replicas in the nodegroup.<br> |

## AzureConfig

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| tenantID | `string` |  |  |
| subscriptionID | `string` |  |  |
| aadApplication | [`AADApplication`](#aadapplication) |  |  |
| location | `string` |  |  |
| storageAccount | `string` |  |  |
| workloadIdentity | [`WorkloadIdentity`](#workloadidentity) |  |  |
| sshPublicKey | `string` |  |  |
| canonicalUbuntuImage | [`CanonicalUbuntuImage`](#canonicalubuntuimage) |  |  |
| controlPlane | [`AzureControlPlane`](#azurecontrolplane) |  |  |
| nodeGroups | [][`AzureAutoScalableNodeGroup`](#azureautoscalablenodegroup) |  |  |

## AzureControlPlane

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| loadBalancerType | `string` | Public |  |
| diskSizeGB | `uint32` |  |  |
| vmSize | `string` |  |  |
| replicas | `uint32` |  |  |

## AzureCredentials

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| clientID | `string` |  |  |
| clientSecret | `string` |  |  |

## BareMetalConfig

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| ssh | [`BareMetalSSHConfig`](#baremetalsshconfig) |  |  |
| kubelet | [`BareMetalKubeletConfig`](#baremetalkubeletconfig) |  | Kubelet tuning applied to every host (control-plane and workers).<br> |
| controlPlane | [`BareMetalControlPlane`](#baremetalcontrolplane) |  |  |
| nodeGroups | [][`BareMetalNodeGroup`](#baremetalnodegroup) |  |  |

## BareMetalControlPlane

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| endpoint | [`BareMetalControlPlaneEndpoint`](#baremetalcontrolplaneendpoint) |  |  |
| hosts | [][`BareMetalHost`](#baremetalhost) |  |  |

## BareMetalControlPlaneEndpoint

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| host | `string` |  |  |
| port | `uint` | 6443 |  |

## BareMetalHost

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| publicAddress | `string` |  |  |
| privateAddress | `string` |  |  |
| ssh | [`BareMetalSSHConfig`](#baremetalsshconfig) |  |  |

## BareMetalKubeletConfig

<p>BareMetalKubeletConfig mirrors KubeOne's per-host KubeletConfig.
REFER : https://docs.kubermatic.com/kubeone/v1.13/references/kubeone-cluster-v1beta2/#kubeletconfig</p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| systemReserved,omitempty | `map[string]string` |  |  |
| kubeReserved,omitempty | `map[string]string` |  |  |
| evictionHard,omitempty | `map[string]string` |  |  |
| maxPods,omitempty | `int32` |  |  |

## BareMetalNodeGroup

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| hosts | [][`BareMetalHost`](#baremetalhost) |  |  |
| name | `string` |  | Nodegroup name.<br> |
| labels | `map[string]string` | [] | Labels that you want to be propagated to each node in the nodegroup.<br><br>Each label should meet one of the following criterias to propagate to each of the nodes :<br><br>  1. Has node-role.kubernetes.io as prefix.<br>  2. Belongs to node-restriction.kubernetes.io domain.<br>  3. Belongs to node.cluster.x-k8s.io domain.<br><br>REFER : https://cluster-api.sigs.k8s.io/developer/architecture/controllers/metadata-propagation#machine.<br> |
| taints | []`k8s.io/api/core/v1.Taint` | [] | Taints that you want to be propagated to each node in the nodegroup.<br> |

## BareMetalSSHConfig

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| port | `uint` | 22 |  |
| privateKeyFilePath | `string` |  | PrivateKeyFilePath is the on-disk SSH private key<br>kubeaid-cli reads to derive PublicKey + Fingerprint and<br>(for cloud-side SSH connections like the Hetzner NAT<br>gateway setup) to authenticate the SSH session. Required<br>when UseSSHAgent is false; ignored when UseSSHAgent is<br>true (the agent owns the private key — yubikey case —<br>so there's nothing on disk to point at). Cross-field<br>validation in pkg/config/parser/validate.go enforces<br>"exactly one is set".<br> |
| useSSHAgent | `bool` |  | UseSSHAgent flips the SSH key sourcing from "read a file<br>from PrivateKeyFilePath" to "dial $SSH_AUTH_SOCK and ask<br>the agent for its loaded identities". The first identity<br>supplies PublicKey + Fingerprint; the SSH client (kubeone)<br>signs through the agent socket so yubikey-resident<br>private keys never need to be exported.<br> |

## CanonicalUbuntuImage

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| offer | `string` |  |  |
| sku | `string` |  |  |

## CloudConfig

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| aws | [`AWSConfig`](#awsconfig) |  |  |
| azure | [`AzureConfig`](#azureconfig) |  |  |
| hetzner | [`HetznerConfig`](#hetznerconfig) |  |  |
| bare-metal | [`BareMetalConfig`](#baremetalconfig) |  |  |
| local | [`LocalConfig`](#localconfig) |  |  |
| disasterRecovery | [`DisasterRecoveryConfig`](#disasterrecoveryconfig) |  |  |

## ClusterConfig

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| type | `string` | workload |  |
| name | `string` |  | Name of the Kubernetes cluster.<br><br>We don't allow using dots in the cluster name, since it can cause issues with tools like<br>ClusterAPI and Cilium : which use the cluster name to generate other configurations.<br> |
| k8sVersion | `string` |  | Kubernetes version (>= 1.30.0).<br> |
| enableAuditLogging | `bool` | True | Whether you would like to enable Kubernetes Audit Logging out of the box.<br>Suitable Kubernetes API configurations will be done for you automatically. And they can be<br>changed using the apiSever struct field.<br> |
| acmeEmail | `string` |  | ACMEEmail is the contact email used to register with the ACME<br>CA (Let's Encrypt) when cert-manager's ClusterIssuer is<br>rendered. Required when cluster.keycloak.mode=managed (the<br>keycloakx and netbird-mgmt Ingresses both need TLS certs);<br>optional otherwise. Used as Issuer.spec.acme.email.<br> |
| acmeDNS01 | [`ACMEDNS01Config`](#acmedns01config) |  | ACMEDNS01 switches the rendered ClusterIssuer's solver from<br>the HTTP-01 default to DNS-01. Required for the split-horizon<br>mesh pattern: NetBird-exposed services use real public DNS<br>names (e.g. argocd.staging.acme.com) that only resolve inside<br>the mesh — Let's Encrypt can never reach them over HTTP, but<br>proves ownership via a TXT record on the public zone instead.<br>Requires cluster.acmeEmail plus the provider credential in<br>secrets.yaml (acme.cloudflareApiToken).<br> |
| apiServer | [`APIServerConfig`](#apiserverconfig) |  | Configuration options for the Kubernetes API server.<br> |
| lockdown | `bool` |  | Lockdown pre-answers the end-of-bootstrap Host Firewall (CCNP)<br>step. nil = ask interactively (legacy behavior); true = apply<br>without prompting (CI-safe); false = skip the step.<br> |
| keycloak | [`KeycloakConfig`](#keycloakconfig) |  | Keycloak declares the Keycloak instance a VPN cluster hosts as<br>NetBird's SSO IdP. Required on cluster.type=vpn (mode=managed →<br>kubeaid-cli installs it; mode=external → operator runs it<br>elsewhere). Not supported on workload clusters — access there is<br>via the NetBird mesh (cluster.netbird.dns), so a keycloak block<br>on a workload cluster is rejected.<br> |
| netbird | [`NetBirdConfig`](#netbirdconfig) |  | NetBird declares the NetBird Management instance this VPN<br>cluster hosts. Only meaningful when cluster.type=vpn AND<br>cluster.keycloak.mode=managed. NetBird Mgmt's OIDC client<br>is created in the same Keycloak realm; its public DNS is<br>used for the redirect URI and audience claim.<br> |
| additionalUsers | [][`UserConfig`](#userconfig) |  | Other than the root user, addtional users that you would like to be created in each node.<br>NOTE : Currently, we can't register additional SSH key-pairs against the root user.<br> |
| argoCD | [`ArgoCDConfig`](#argocdconfig) |  | ArgoCD specific details.<br> |

## DeployKeysConfig

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| kubeaid | [`SSHKeyPairConfig`](#sshkeypairconfig) |  |  |
| kubeaidConfig | [`SSHKeyPairConfig`](#sshkeypairconfig) |  |  |

## DisasterRecoveryConfig

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| veleroBackupsBucketName | `string` |  |  |
| sealedSecretsBackupsBucketName | `string` |  |  |

## FileConfig

<p>REFER : "sigs.k8s.io/cluster-api/bootstrap/kubeadm/api/v1beta1".File.</p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| path | `string` |  |  |
| content | `string` |  |  |

## FirewallConfig

<p>FirewallConfig drives the Cilium host-firewall policy rendered by kubeaid-cli
for Hetzner bare-metal clusters. The resulting CiliumClusterwideNetworkPolicy
selects every node and locks down the public NIC via eBPF host-endpoint rules.
See docs/hetzner-bare-metal-network-surface.md.</p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| enabled | `bool` |  | Enabled gates whether kubeaid-cli renders the Cilium host-firewall<br>CiliumClusterwideNetworkPolicy at all. Defaults to true; set false to<br>opt out — e.g. a separate upstream L3 firewall appliance already fronts<br>the cluster. A pointer so an explicit "enabled: false" is distinguishable<br>from unset and honoured.<br> |
| allowSshFrom | []`string` |  | AllowSSHFrom restricts inbound SSH (22/tcp) on every bare-metal node to<br>these sources. Rendered as a fromCIDR rule in the CCNP. Empty (the<br>default) allows SSH from anywhere — matching the bare-metal posture where<br>nodes are not NetBird peers and have no mesh fallback path. Each entry is<br>an IPv4 address or CIDR (e.g. "203.0.113.4" or "203.0.113.0/24"); a bare<br>address is treated as /32.<br> |
| allowPublic | [][`FirewallPort`](#firewallport) |  | AllowPublic is a legacy field from the (removed) Hetzner Robot firewall.<br>It is parsed and validated but NOT rendered into the Cilium host-firewall<br>policy — parser.validateHetznerConfig only logs a warning when it is set.<br>The policy's world-facing ports come from hostNetworkPolicy.publicPorts<br>(chart default [80, 443]); 6443 is never world-public — it is a separate<br>rule restricted to hostNetworkPolicy.apiserverSourceCIDRs (the node IPs).<br>To open extra ports to the world, add them to hostNetworkPolicy.publicPorts<br>in the cilium chart values overlay, not here.<br> |

## FirewallPort

<p>FirewallPort is one {port, protocol} entry in FirewallConfig.AllowPublic.</p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| port | `string` |  | Port is a single port ("25") or an inclusive range ("30000-32767").<br> |
| protocol | `string` |  | Protocol is "tcp", "udp", or omitted for any protocol.<br> |

## ForksConfig

<p>KubeAid and KubeAid Config repository specific details.
We require the KubeAid and KubeAid Config repositories to be hosted in the same Git server.</p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| kubeaid | [`KubeAidForkConfig`](#kubeaidforkconfig) |  | KubeAid repository specific details.<br> |
| kubeaidConfig | [`KubeaidConfigForkConfig`](#kubeaidconfigforkconfig) |  | KubeAid Config repository specific details.<br> |

## GeneralConfig

<p>Non secret configuration options.</p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| git | [`GitConfig`](#gitconfig) |  | Git server specific details.<br> |
| forkURLs | [`ForksConfig`](#forksconfig) |  | KubeAid and KubeAid Config repository specific details.<br>The KubeAid and KubeAid Config repositories must be hosted in the same Git server.<br> |
| cluster | [`ClusterConfig`](#clusterconfig) |  | Kubernetes specific details.<br> |
| cloud | [`CloudConfig`](#cloudconfig) |  | Cloud provider specific details.<br> |
| kubePrometheus | [`KubePrometheusConfig`](#kubeprometheusconfig) |  | Kube Prometheus installation specific details.<br> |
| kubeaidStoragectl | [`KubeaidStoragectlConfig`](#kubeaidstoragectlconfig) |  | KubeaidStoragectl pins the kubeaid-storagectl release tag<br>used by the bare-metal preKubeadm script when carving the<br>ZFS pool and Ceph partition. Leave nil (block omitted) to<br>fall back to the kubeaid-cli binary's own release version,<br>which is the right default for most operators — every node<br>downloads the storagectl that ships with the kubeaid-cli<br>release that bootstrapped it. Set explicitly to override:<br><br>  - to pin against a tag newer/older than kubeaid-cli for<br>    testing a fix or rolling back, or<br>  - to point at an unreleased dev build when running a<br>    `go run ./cmd/kubeaid-cli` development bootstrap (the<br>    CLI's KubeaidCLIVersion is empty there and the chart<br>    would otherwise fall through to `latest`, which 404s if<br>    no release has been published yet).<br> |
| obmondo | [`ObmondoConfig`](#obmondoconfig) |  | Obmondo customer specific details.<br> |

## GitConfig

<p>Git specific details, used by KubeAid CLI,
to clone repositories from and push changes to the Git server.
We enforce the user to use SSH, for authenticating to the Git server.</p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| caBundlePath | `string` |  |  |
| sshUsername | `string` | git | SSH username.<br> |
| knownHosts | []`string` |  | Additional SSH known hosts.<br>Merged with known hosts of common Git repo hosting providers (like Azure DevOps, GitLab etc.)<br> |
| privateKeyFilePath | `string` |  | PrivateKeyFilePath is the on-disk SSH private key<br>kubeaid-cli reads to derive PublicKey + Fingerprint and<br>(for cloud-side SSH connections like the Hetzner NAT<br>gateway setup) to authenticate the SSH session. Required<br>when UseSSHAgent is false; ignored when UseSSHAgent is<br>true (the agent owns the private key — yubikey case —<br>so there's nothing on disk to point at). Cross-field<br>validation in pkg/config/parser/validate.go enforces<br>"exactly one is set".<br> |
| useSSHAgent | `bool` |  | UseSSHAgent flips the SSH key sourcing from "read a file<br>from PrivateKeyFilePath" to "dial $SSH_AUTH_SOCK and ask<br>the agent for its loaded identities". The first identity<br>supplies PublicKey + Fingerprint; the SSH client (kubeone)<br>signs through the agent socket so yubikey-resident<br>private keys never need to be exported.<br> |

## HCloudAutoScalableNodeGroup

<p>Details about (autoscalable) node-groups in HCloud.</p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| machineType | `string` |  | HCloud machine type.<br>You can browse all available HCloud machine types here : https://hetzner.com/cloud.<br> |
| name | `string` |  | Nodegroup name.<br> |
| labels | `map[string]string` | [] | Labels that you want to be propagated to each node in the nodegroup.<br><br>Each label should meet one of the following criterias to propagate to each of the nodes :<br><br>  1. Has node-role.kubernetes.io as prefix.<br>  2. Belongs to node-restriction.kubernetes.io domain.<br>  3. Belongs to node.cluster.x-k8s.io domain.<br><br>REFER : https://cluster-api.sigs.k8s.io/developer/architecture/controllers/metadata-propagation#machine.<br> |
| taints | []`k8s.io/api/core/v1.Taint` | [] | Taints that you want to be propagated to each node in the nodegroup.<br> |
| minSize | `uint` |  | Minimum number of replicas in the nodegroup.<br> |
| maxSize | `uint` |  | Maximum number of replicas in the nodegroup.<br> |

## HCloudConfig

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| zone | `string` |  |  |
| imageName | `string` | ubuntu-26.04 |  |
| natGatewayServerType | `string` | cpx22 | NATGatewayServerType is the HCloud server type for the NAT gateway<br>that fronts the private network during bootstrap. cpx22 is a small,<br>cost-optimised x86 box — ample for NAT. Override it if cpx22 is out<br>of stock / not offered in your locations, or you need more throughput<br>(`hcloud server-type list` shows what's available).<br> |
| hetznerNetwork | [`HetznerNetworkConfig`](#hetznernetworkconfig) |  | Hetzner Network specific details.<br> |

## HCloudControlPlane

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| machineType | `string` |  |  |
| replicas | `uint` |  |  |
| loadBalancer | [`HCloudControlPlaneLoadBalancer`](#hcloudcontrolplaneloadbalancer) |  |  |

## HCloudControlPlaneLoadBalancer

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| enabled | `bool` |  |  |
| region | `string` |  |  |
| endpoint | `string` |  | Endpoint is the FQDN clients use to reach kube-apiserver<br>(CAPI's controlPlaneEndpoint.host, kubeadm cert SAN,<br>kubeconfig server URL). Optional: when omitted, the LB<br>private IP is used as the control-plane endpoint directly<br>(no public interface, no DNS wait). When set, the LB gets<br>a public interface during bootstrap and kubeaid-cli waits<br>for the operator's DNS A-record to land before continuing.<br>DNS resolution is the operator's responsibility.<br> |

## HCloudVPNClusterConfig

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| name | `string` |  |  |

## HetznerBareMetalConfig

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| wipeDisks | `bool` | false |  |
| installImage | [`InstallImageConfig`](#installimageconfig) |  |  |
| firewall | [`FirewallConfig`](#firewallconfig) |  | Firewall configures the Cilium host-firewall policy (CiliumClusterwideNetworkPolicy)<br>that locks down each bare-metal node's public NIC. Enabled controls whether<br>kubeaid-cli renders the policy at all; AllowSSHFrom feeds the per-CIDR SSH ingress<br>rule. See docs/hetzner-bare-metal-network-surface.md.<br> |
| zfs | [`ZFSConfig`](#zfsconfig) |  | ZFS specific configuration.<br>Every node runs a ZFS pool, named primary. We carve out storage for container images, pod<br>logs and pod ephemeral volumes from that ZFS pool, as required.<br>The ZFS pool has RAIDZ-1 enabled, which means it can survive single disk failure.<br> |
| vSwitch | [`VSwitchConfig`](#vswitchconfig) |  | Details about the VSwitch which'll be used to connect the Hetzner Bare Metal servers with<br>the Hetzner Network.<br> |

## HetznerBareMetalControlPlane

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| endpoint | [`HetznerBareMetalControlPlaneEndpoint`](#hetznerbaremetalcontrolplaneendpoint) |  |  |
| bareMetalHosts | [][`HetznerBareMetalHost`](#hetznerbaremetalhost) |  |  |
| zfs | [`ZFSConfig`](#zfsconfig) |  | ZFS pool size on each control-plane node. See ZFSConfig.Size for sizing rules.<br> |

## HetznerBareMetalControlPlaneEndpoint

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| isFailoverIP | `bool` |  |  |
| host | `string` |  |  |

## HetznerBareMetalHost

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| serverID | `string` |  |  |
| privateIP | `string` |  |  |

## HetznerBareMetalNodeGroup

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| bareMetalHosts | [][`HetznerBareMetalHost`](#hetznerbaremetalhost) |  |  |
| zfs | [`ZFSConfig`](#zfsconfig) |  | ZFS specific configuration.<br>Every node runs a ZFS pool, named primary. We carve out storage for container images, pod<br>logs and pod ephemeral volumes from that ZFS pool, as required.<br>The ZFS pool has RAIDZ-1 enabled, which means it can survive single disk failure.<br> |
| name | `string` |  | Nodegroup name.<br> |
| labels | `map[string]string` | [] | Labels that you want to be propagated to each node in the nodegroup.<br><br>Each label should meet one of the following criterias to propagate to each of the nodes :<br><br>  1. Has node-role.kubernetes.io as prefix.<br>  2. Belongs to node-restriction.kubernetes.io domain.<br>  3. Belongs to node.cluster.x-k8s.io domain.<br><br>REFER : https://cluster-api.sigs.k8s.io/developer/architecture/controllers/metadata-propagation#machine.<br> |
| taints | []`k8s.io/api/core/v1.Taint` | [] | Taints that you want to be propagated to each node in the nodegroup.<br> |

## HetznerConfig

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| mode | `string` | hcloud | 			The Hetzner mode to use :<br><br>			  (1) hcloud : Both the control-plane and the nodegroups will be in HCloud.<br><br>			  (2) bare-metal : Both the control-plane and the nodegroups will be in Hetzner Bare Metal.<br><br>			  (3) hybrid : The control-plane will be in HCloud, and each node-group can be either in<br>			               HCloud or Hetzner Bare Metal.<br> |
| hcloudVPNCluster | [`HCloudVPNClusterConfig`](#hcloudvpnclusterconfig) |  | Details about the VPN cluster you have in HCloud.<br> |
| sshKeyPair | [`HetznerSSHKeyPair`](#hetznersshkeypair) |  | Details about the SSH keypair which will be used to SSH into the HCloud or / and Hetzner<br>Bare Metal server.<br>KubeAid CLI will create the corresponding HCloud or / and Hetzner Bare Metal SSH keypairs,<br>if it / they doesn't already exist.<br> |
| hcloud | [`HCloudConfig`](#hcloudconfig) |  | HCloud specific details.<br> |
| bareMetal | [`HetznerBareMetalConfig`](#hetznerbaremetalconfig) |  | Hetzner bare-metal specific details.<br> |
| controlPlane | [`HetznerControlPlane`](#hetznercontrolplane) |  | Control-plane specific details.<br> |
| nodeGroups | [`HetznerNodeGroups`](#hetznernodegroups) |  | Details about the node-groups.<br> |

## HetznerControlPlane

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| hcloud | [`HCloudControlPlane`](#hcloudcontrolplane) |  |  |
| bareMetal | [`HetznerBareMetalControlPlane`](#hetznerbaremetalcontrolplane) |  |  |
| regions | []`string` |  | Regions is the list of Hetzner regions (lower-case IDs: "fsn1", "hel1", "ash", ...)<br>the CAPH chart constrains control-plane placement to. At least one is required.<br> |
| extraCertSANs,omitempty | []`string` |  | ExtraCertSANs are additional DNS names added to the apiserver's<br>TLS cert SAN list, on every Hetzner mode (hcloud, bare-metal,<br>hybrid). The chart merges these with endpoint.host into kubeadm's<br>apiServer.certSANs. Use for any additional hostnames clients reach<br>the apiserver under.<br> |

## HetznerCredentials

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| apiToken | `string` |  | APIToken is the HCloud Cloud-API token. Required for every Hetzner mode.<br> |
| robot | [`HetznerRobotCredentials`](#hetznerrobotcredentials) |  |  |

## HetznerNetworkConfig

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| cidr | `string` |  |  |
| hcloudServersSubnetCIDR | `string` |  |  |

## HetznerNodeGroups

<p>Details about node-groups in Hetzner.</p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| hcloud | [][`HCloudAutoScalableNodeGroup`](#hcloudautoscalablenodegroup) |  | Details about node-groups in HCloud.<br> |
| bareMetal | [][`HetznerBareMetalNodeGroup`](#hetznerbaremetalnodegroup) |  | Details about node-groups in Hetzner Bare Metal.<br> |

## HetznerRobotCredentials

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| user | `string` |  |  |
| password | `string` |  |  |

## HetznerSSHKeyPair

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| name | `string` |  |  |
| privateKeyFilePath | `string` |  | PrivateKeyFilePath is the on-disk SSH private key<br>kubeaid-cli reads to derive PublicKey + Fingerprint and<br>(for cloud-side SSH connections like the Hetzner NAT<br>gateway setup) to authenticate the SSH session. Required<br>when UseSSHAgent is false; ignored when UseSSHAgent is<br>true (the agent owns the private key — yubikey case —<br>so there's nothing on disk to point at). Cross-field<br>validation in pkg/config/parser/validate.go enforces<br>"exactly one is set".<br> |
| useSSHAgent | `bool` |  | UseSSHAgent flips the SSH key sourcing from "read a file<br>from PrivateKeyFilePath" to "dial $SSH_AUTH_SOCK and ask<br>the agent for its loaded identities". The first identity<br>supplies PublicKey + Fingerprint; the SSH client (kubeone)<br>signs through the agent socket so yubikey-resident<br>private keys never need to be exported.<br> |

## HostPathMountConfig

<p>REFER : "sigs.k8s.io/cluster-api/bootstrap/kubeadm/api/v1beta1".HostPathMount</p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| name | `string` |  |  |
| hostPath | `string` |  |  |
| mountPath | `string` |  |  |
| pathType | `k8s.io/api/core/v1.HostPathType` |  |  |
| readOnly | `bool` | true | Whether the mount should be read-only.<br> |

## InstallImageConfig

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| imagePath | `string` | /root/.oldroot/nfs/images/Ubuntu-2604-resolute-amd64-base.tar.zst |  |
| vg0 | [`VG0Config`](#vg0config) |  |  |

## KeycloakConfig

<p>KeycloakConfig declares the Keycloak instance a VPN cluster hosts as
NetBird's SSO IdP. The parser derives the Realm from DNS when unset
and validates the combination against cluster.type. The admin
password is generated by kubeaid-cli at bootstrap and never lives in
this struct or in secrets.yaml; only Mode/DNS/Realm are user-facing.</p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| mode | `string` |  | Mode is "managed" (kubeaid-cli installs Keycloak via the<br>keycloakx Helm chart on this cluster — VPN clusters only)<br>or "external" (Keycloak is already running elsewhere;<br>supply DNS only). Workload clusters must use external.<br> |
| dns | `string` |  | DNS is the public hostname Keycloak is reachable at, e.g.<br>"keycloak.vpn.acme.com". Required. Used to derive the OIDC<br>issuer URL the apiserver and kubelogin trust, and (when<br>Realm is unset) to default the realm name.<br> |
| realm | `string` |  | Realm is the Keycloak realm name. Optional — when empty,<br>kubeaid-cli derives it from DNS via<br>`golang.org/x/net/publicsuffix.EffectiveTLDPlusOne` and the<br>first dot-separated segment of the result. Examples:<br>  keycloak.vpn.acme.com  → "acme"<br>  keycloak.foo.co.uk     → "foo"<br>Set this explicitly to override the derivation.<br> |

## KeycloakCredentials

<p>KeycloakCredentials carries Keycloak-related secrets — admin
credentials and OIDC client secrets the operator either
supplies (external mode) or kubeaid-cli auto-generates and
persists into secrets.yaml (managed mode, via FillMissingSecrets).

All fields are persisted in secrets.yaml so SealedSecret
renders are byte-stable across re-runs — the alternative
(read-or-generate from the in-cluster Secret) caused the
re-encryption noise that produced spurious PRs on every run.</p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| adminPassword | `string` |  | AdminPassword is templated into the keycloak-admin<br>SealedSecret's KEYCLOAK_PASSWORD key. Required when<br>cluster.keycloak.mode is "managed"; ignored otherwise.<br>FillMissingSecrets generates a value here on first run if<br>the field is empty.<br> |
| netBirdBackendClientSecret | `string` |  | NetBirdBackendClientSecret is the confidential-client<br>secret for the netbird-backend OIDC client. In external<br>mode the operator creates the client in their Keycloak<br>and supplies the resulting secret here. In managed mode<br>FillMissingSecrets generates it, the realm reconciler<br>creates the Keycloak client with this exact value, and<br>the netbird SealedSecret is templated with the same<br>value — single source of truth either way.<br> |

## KubeAidForkConfig

<p>KubeAid repository specific details.</p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| url | `string` |  | KubeAid repository SSH URL.<br> |
| version | `string` |  | KubeAid git ref (tag / branch / commit).<br> |

## KubePrometheusConfig

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| version | `string` |  |  |
| grafanaURL | `string` |  |  |

## KubeaidConfigForkConfig

<p>KubeAid Config repository specific details.</p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| url | `string` |  | KubeAid Config repository SSH URL.<br> |
| directory | `string` |  | Name of the directory inside your KubeAid Config repository's k8s folder, where the KubeAid<br>Config files for this cluster will be contained.<br><br>When not specified, the directory name will default to the cluster name.<br><br>So, suppose your cluster name is 'staging'. Then, the directory name will default to<br>'staging'. Or you can customize it to something like 'staging.qa'.<br> |

## KubeaidStoragectlConfig

<p>KubeaidStoragectlConfig pins the kubeaid-storagectl release.
See GeneralConfig.KubeaidStoragectl for when to set it.</p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| version | `string` |  | Version is the GitHub release tag of kubeaid-storagectl —<br>rendered into the chart as `global.kubeaidStoragectl.version`<br>and used to build the `releases/download/<version>/` URL the<br>node's preKubeadm wget hits. Empty string is treated as "not<br>set" and falls back to kubeaid-cli's own version, same as<br>omitting the parent block.<br> |

## LocalConfig

<p>Local specific.</p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|

## NetBirdClusterProxyConfig

<p>NetBirdClusterProxyConfig configures the netbird-operator kube-apiserver
proxy (netbird-operator.clusterProxy in the chart values). The proxy
registers under cluster.name (netbird kubernetes write-kubeconfig
<cluster.name>).</p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| enabled | `bool` |  | Enabled toggles the cluster proxy.<br> |
| rbac | [][`NetBirdClusterProxyRBACConfig`](#netbirdclusterproxyrbacconfig) |  | RBAC binds NetBird groups to cluster roles via the proxy's<br>identity impersonation.<br> |

## NetBirdClusterProxyRBACConfig

<p>NetBirdClusterProxyRBACConfig binds one NetBird group to one
ClusterRole through the cluster proxy.</p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| group | `string` |  |  |
| clusterRole | `string` |  |  |

## NetBirdConfig

<p>NetBirdConfig describes this cluster's relationship to the NetBird
mesh. It is valid for both cluster.type=vpn (which hosts NetBird
Mgmt) and cluster.type=workload (which only joins the mesh):
dns/stun/turn are meaningful only on the VPN host, while dnsZone
applies to any cluster on the mesh. cluster.type is the gate.</p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| dns | `string` |  | DNS is the public hostname NetBird Management is reachable at,<br>e.g. "netbird.vpn.acme.com". Required only for cluster.type=vpn<br>(enforced in parser/keycloak.go); unused on workload clusters.<br> |
| dnsZone | `string` |  | DNSZone is the mesh DNS domain peers resolve under — NetBird<br>Mgmt's --dns-domain, e.g. "mesh.acme.com". Operator-supplied, no<br>default. Required for cluster.type=vpn and for workload clusters<br>that join a mesh; absent on workload clusters that don't. Used to<br>create the DNS zone on NetBird, to drive --dns-domain on VPN<br>clusters, and to add the kubernetes.<dnsZone> apiserver cert SAN.<br> |
| stunDNS | `string` |  | StunDNS is the public hostname Coturn answers STUN queries<br>on, e.g. "stun.vpn.acme.com". Optional: kubeaid-cli derives<br>it as "stun.<base>" where base is DNS with the leading<br>"netbird." stripped (so netbird.vpn.acme.com → stun.vpn.acme.com).<br>Override only when STUN is exposed on a non-standard FQDN.<br> |
| turnDNS | `string` |  | TurnDNS is the public hostname Coturn answers TURN queries<br>on, e.g. "turn.vpn.acme.com". Optional: derived as<br>"turn.<base>" by the same logic as StunDNS.<br> |
| turnUser | `string` | netbird | TurnUser is the static username Coturn / NetBird Mgmt agree<br>on for TURN authentication. The matching password is<br>generated and persisted in the Secret. Optional, defaults<br>to "netbird".<br> |
| clusterProxy | [`NetBirdClusterProxyConfig`](#netbirdclusterproxyconfig) |  | ClusterProxy configures the netbird-operator's kube-apiserver<br>proxy (operator >= 0.7.0): a mesh peer that proxies kubectl to<br>the in-cluster apiserver, impersonating the caller's NetBird<br>identity. Omit the block to leave it disabled.<br> |
| groups | []`string` |  | Groups are extra NetBird groups this cluster OWNS (chart: groups), beyond<br>the derived k8s-<cluster> and k8s-<cluster>-access. Declare a group from ONE<br>cluster only — a duplicate wedges that operator on HTTP 409.<br> |

## NetBirdCredentials

<p>NetBirdCredentials carries the random secrets NetBird's
in-cluster components need at startup. All fields are
auto-generated by FillMissingSecrets when blank. Persisted
in secrets.yaml for re-run stability — same rationale as
KeycloakCredentials.</p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| datastoreEncryptionKey | `string` |  | DatastoreEncryptionKey is the AES key NetBird Mgmt uses<br>to encrypt its data store. base64(32 random bytes); the<br>chart base64-decodes it back to 32 raw bytes for<br>AES-256.<br> |
| relayPassword | `string` |  | RelayPassword is the shared secret between NetBird Mgmt<br>and the in-cluster Relay deployment. Alphanumeric so it<br>flows cleanly through Helm values + envFrom.<br> |
| turnPassword | `string` |  | TurnPassword is the credential the NetBird agents use to<br>authenticate with Coturn (TURN server). Same value is<br>templated into both the netbird Secret (Mgmt-side) and<br>the netbird-turn-credentials Secret (Coturn-side) — the<br>two MUST match or relayed TURN auth fails.<br> |
| apiKey | `string` |  | APIKey is a NetBird Management service-user access token<br>(nbp_…) the netbird-operator authenticates to the Mgmt<br>API with — minting setup keys for routing peers, managing<br>groups / networks / policies. Created manually in the<br>NetBird dashboard: Team → Service Users → create →<br>generate access token (a service user, not a personal<br>PAT, so it survives offboarding). NOT auto-generated by<br>FillMissingSecrets — only the Mgmt dashboard can mint it.<br>Rendered into the netbird/netbird-mgmt-api-key<br>SealedSecret whose NB_API_KEY the operator Deployment<br>reads (the chart's default secret ref). When blank,<br>the SealedSecret is skipped and bootstrap pauses at<br>netbird.AwaitOperatorToken with instructions instead.<br> |

## NodeGroup

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| name | `string` |  | Nodegroup name.<br> |
| labels | `map[string]string` | [] | Labels that you want to be propagated to each node in the nodegroup.<br><br>Each label should meet one of the following criterias to propagate to each of the nodes :<br><br>  1. Has node-role.kubernetes.io as prefix.<br>  2. Belongs to node-restriction.kubernetes.io domain.<br>  3. Belongs to node.cluster.x-k8s.io domain.<br><br>REFER : https://cluster-api.sigs.k8s.io/developer/architecture/controllers/metadata-propagation#machine.<br> |
| taints | []`k8s.io/api/core/v1.Taint` | [] | Taints that you want to be propagated to each node in the nodegroup.<br> |

## ObmondoConfig

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| customerID | `string` |  |  |
| monitoring | `bool` |  |  |
| certPath | `string` |  | Path to the mTLS client cert issued by Obmondo. Required when<br>Monitoring is true — kubeaid-agent uses it to authenticate to the<br>Obmondo API, and kube-prometheus's Alertmanager uses it to push<br>alerts to Obmondo's alert-receiver endpoint.<br> |
| keyPath | `string` |  | Path to the private key paired with CertPath. Required when<br>Monitoring is true.<br> |

## OpenIDProviderSSHKeyPairConfig

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| publicKeyFilePath | `string` |  |  |
| privateKeyFilePath | `string` |  | PrivateKeyFilePath is the on-disk SSH private key<br>kubeaid-cli reads to derive PublicKey + Fingerprint and<br>(for cloud-side SSH connections like the Hetzner NAT<br>gateway setup) to authenticate the SSH session. Required<br>when UseSSHAgent is false; ignored when UseSSHAgent is<br>true (the agent owns the private key — yubikey case —<br>so there's nothing on disk to point at). Cross-field<br>validation in pkg/config/parser/validate.go enforces<br>"exactly one is set".<br> |
| useSSHAgent | `bool` |  | UseSSHAgent flips the SSH key sourcing from "read a file<br>from PrivateKeyFilePath" to "dial $SSH_AUTH_SOCK and ask<br>the agent for its loaded identities". The first identity<br>supplies PublicKey + Fingerprint; the SSH client (kubeone)<br>signs through the agent socket so yubikey-resident<br>private keys never need to be exported.<br> |

## SSHKeyPairConfig

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| privateKeyFilePath | `string` |  | PrivateKeyFilePath is the on-disk SSH private key<br>kubeaid-cli reads to derive PublicKey + Fingerprint and<br>(for cloud-side SSH connections like the Hetzner NAT<br>gateway setup) to authenticate the SSH session. Required<br>when UseSSHAgent is false; ignored when UseSSHAgent is<br>true (the agent owns the private key — yubikey case —<br>so there's nothing on disk to point at). Cross-field<br>validation in pkg/config/parser/validate.go enforces<br>"exactly one is set".<br> |
| useSSHAgent | `bool` |  | UseSSHAgent flips the SSH key sourcing from "read a file<br>from PrivateKeyFilePath" to "dial $SSH_AUTH_SOCK and ask<br>the agent for its loaded identities". The first identity<br>supplies PublicKey + Fingerprint; the SSH client (kubeone)<br>signs through the agent socket so yubikey-resident<br>private keys never need to be exported.<br> |

## SecretsConfig

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| aws | [`AWSCredentials`](#awscredentials) |  |  |
| azure | [`AzureCredentials`](#azurecredentials) |  |  |
| hetzner | [`HetznerCredentials`](#hetznercredentials) |  |  |
| keycloak | [`KeycloakCredentials`](#keycloakcredentials) |  |  |
| netbird | [`NetBirdCredentials`](#netbirdcredentials) |  |  |
| acme | [`ACMECredentials`](#acmecredentials) |  |  |

## UserConfig

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| name | `string` |  |  |
| sshPublicKey | `string` |  |  |

## VG0Config

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| size | `int` | 80 |  |
| rootVolumeSize | `int` | 50 |  |

## VSwitchConfig

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| vlanID | `int` |  |  |
| name | `string` |  |  |
| subnetCIDRBlock | `string` |  | SubnetCIDRBlock is the vSwitch subnet attached to the Hetzner Network.<br>The IP written here doubles as the subnet's gateway (net.ParseCIDR's<br>first return), so "10.0.1.0/24" yields gateway 10.0.1.0 — write the IP<br>you want as the gateway, not just any address in the range.<br> |

## WorkloadIdentity

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| openIDProviderSSHKeyPair | [`OpenIDProviderSSHKeyPairConfig`](#openidprovidersshkeypairconfig) |  |  |

## ZFSConfig

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| size | `int` | 220 | ZFS pool size (in GB), on each node in the corresponding node-group.<br>Must be >= 200 GB : reserving 100 GB for container images, 50 GB for pod logs and 50 GB for<br>pod ephemeral volumes.<br>On top of that, if you want x GB of node-local storage for your workloads (like Redis),<br>the ZFS pool size will be (200 + 2x) GB, keeping in mind that RAIDZ-1 is enabled.<br> |