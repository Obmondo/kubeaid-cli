# Configuration Reference
- [AADApplication](#aadapplication)
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
- [BareMetalNodeGroup](#baremetalnodegroup)
- [BareMetalSSHConfig](#baremetalsshconfig)
- [CanonicalUbuntuImage](#canonicalubuntuimage)
- [CloudConfig](#cloudconfig)
- [ClusterConfig](#clusterconfig)
- [DeployKeysConfig](#deploykeysconfig)
- [DisasterRecoveryConfig](#disasterrecoveryconfig)
- [FileConfig](#fileconfig)
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
- [LocalConfig](#localconfig)
- [NetBirdConfig](#netbirdconfig)
- [NetBirdCredentials](#netbirdcredentials)
- [NodeGroup](#nodegroup)
- [OIDCConfig](#oidcconfig)
- [ObmondoConfig](#obmondoconfig)
- [ObmondoCredentials](#obmondocredentials)
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
| oidc | [`OIDCConfig`](#oidcconfig) |  | OIDC configures kube-apiserver to validate JWTs issued by an<br>external OpenID Connect provider (typically Keycloak). When<br>set, the parser renders a structured AuthenticationConfiguration<br>YAML, writes it via APIServerConfig.Files, and points<br>kube-apiserver at it with --authentication-config. Skipping<br>this block leaves kube-apiserver without OIDC.<br> |

## AWSAutoScalableNodeGroup

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| ami | [`AMIConfig`](#amiconfig) |  |  |
| instanceType | `string` |  |  |
| rootVolumeSize | `uint32` |  |  |
| sshKeyName | `string` |  |  |
| minSize | `uint` |  | Minimum number of replicas in the nodegroup.<br> |
| maxSize | `uint` |  | Maximum number of replicas in the nodegroup.<br> |
| name | `string` |  | Nodegroup name.<br> |
| labels | `map[string]string` | [] | Labels that you want to be propagated to each node in the nodegroup.<br><br>Each label should meet one of the following criterias to propagate to each of the nodes :<br><br>  1. Has node-role.kubernetes.io as prefix.<br>  2. Belongs to node-restriction.kubernetes.io domain.<br>  3. Belongs to node.cluster.x-k8s.io domain.<br><br>REFER : https://cluster-api.sigs.k8s.io/developer/architecture/controllers/metadata-propagation#machine.<br> |
| taints | []`k8s.io/api/core/v1.Taint` | [] | Taints that you want to be propagated to each node in the nodegroup.<br> |

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
| minSize | `uint` |  | Minimum number of replicas in the nodegroup.<br> |
| maxSize | `uint` |  | Maximum number of replicas in the nodegroup.<br> |
| name | `string` |  | Nodegroup name.<br> |
| labels | `map[string]string` | [] | Labels that you want to be propagated to each node in the nodegroup.<br><br>Each label should meet one of the following criterias to propagate to each of the nodes :<br><br>  1. Has node-role.kubernetes.io as prefix.<br>  2. Belongs to node-restriction.kubernetes.io domain.<br>  3. Belongs to node.cluster.x-k8s.io domain.<br><br>REFER : https://cluster-api.sigs.k8s.io/developer/architecture/controllers/metadata-propagation#machine.<br> |
| taints | []`k8s.io/api/core/v1.Taint` | [] | Taints that you want to be propagated to each node in the nodegroup.<br> |

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
| apiServer | [`APIServerConfig`](#apiserverconfig) |  | Configuration options for the Kubernetes API server.<br> |
| keycloak | [`KeycloakConfig`](#keycloakconfig) |  | Keycloak declares the Keycloak instance this cluster<br>authenticates against. Semantics depend on cluster.type:<br><br>  - cluster.type=vpn (required block):<br>      mode=managed  → kubeaid-cli installs Keycloak on<br>                      this cluster.<br>      mode=external → operator runs Keycloak elsewhere.<br><br>  - cluster.type=workload (optional block):<br>      mode=external only → the cluster's kube-apiserver<br>                           trusts this Keycloak for OIDC.<br>                           kubeaid-cli derives<br>                           apiServer.oidc.{issuerUrl,<br>                           clientId} from this block;<br>                           explicit apiServer.oidc still<br>                           wins. Workload clusters never<br>                           host Keycloak — mode=managed is<br>                           rejected.<br><br>Omitting the block on a workload cluster boots it without<br>OIDC; users authenticate with admin.conf (the workload<br>bootstrap prints a warning).<br> |
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
| minSize | `uint` |  | Minimum number of replicas in the nodegroup.<br> |
| maxSize | `uint` |  | Maximum number of replicas in the nodegroup.<br> |
| name | `string` |  | Nodegroup name.<br> |
| labels | `map[string]string` | [] | Labels that you want to be propagated to each node in the nodegroup.<br><br>Each label should meet one of the following criterias to propagate to each of the nodes :<br><br>  1. Has node-role.kubernetes.io as prefix.<br>  2. Belongs to node-restriction.kubernetes.io domain.<br>  3. Belongs to node.cluster.x-k8s.io domain.<br><br>REFER : https://cluster-api.sigs.k8s.io/developer/architecture/controllers/metadata-propagation#machine.<br> |
| taints | []`k8s.io/api/core/v1.Taint` | [] | Taints that you want to be propagated to each node in the nodegroup.<br> |

## HCloudConfig

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| zone | `string` |  |  |
| imageName | `string` | ubuntu-24.04 |  |
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
| endpoint | `string` |  | Endpoint is the FQDN clients use to reach kube-apiserver<br>(CAPI's controlPlaneEndpoint.host, kubeadm cert SAN,<br>kubeconfig server URL). Required. DNS resolution is the<br>operator's responsibility — the LB has both public and<br>private interfaces during bootstrap; once NetBird is up<br>the public is removed and clients reach the private IP<br>through the mesh.<br> |
| extraCertSANs,omitempty | []`string` |  | ExtraCertSANs are additional DNS names included in the<br>apiserver's TLS cert SAN list, alongside Endpoint. Use<br>this for mesh-internal hostnames clients also use to<br>reach the apiserver — e.g. a NetBird-assigned name like<br>"netbird.k8s-api" that resolves through NetBird DNS to<br>the LB private IP. Without these, kubectl via the mesh<br>hostname fails with an x509 cert-name mismatch.<br> |

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

<p>KeycloakConfig declares the OIDC provider for this cluster. The
parser hydrates derived fields (Realm from DNS, the apiServer.oidc
block) and validates the combination against cluster.type. The
admin password is generated by kubeaid-cli at bootstrap and never
lives in this struct or in secrets.yaml; only Mode/DNS/Realm are
user-facing.</p>

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

## LocalConfig

<p>Local specific.</p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|

## NetBirdConfig

<p>NetBirdConfig declares the NetBird Management instance this
VPN cluster hosts. Used to render the redirect URI and
audience claim for the netbird-client / netbird-backend OIDC
clients in Keycloak, and (when this VPN cluster also hosts
Coturn / Relay) to compute the public STUN / TURN endpoints
kubeaid-cli writes into the netbird Secret.</p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| dns | `string` |  | DNS is the public hostname NetBird Management is<br>reachable at, e.g. "netbird.vpn.acme.com". Required.<br> |
| stunDNS | `string` |  | StunDNS is the public hostname Coturn answers STUN queries<br>on, e.g. "stun.vpn.acme.com". Optional: kubeaid-cli derives<br>it as "stun.<base>" where base is DNS with the leading<br>"netbird." stripped (so netbird.vpn.acme.com → stun.vpn.acme.com).<br>Override only when STUN is exposed on a non-standard FQDN.<br> |
| turnDNS | `string` |  | TurnDNS is the public hostname Coturn answers TURN queries<br>on, e.g. "turn.vpn.acme.com". Optional: derived as<br>"turn.<base>" by the same logic as StunDNS.<br> |
| turnUser | `string` | netbird | TurnUser is the static username Coturn / NetBird Mgmt agree<br>on for TURN authentication. The matching password is<br>generated and persisted in the Secret. Optional, defaults<br>to "netbird".<br> |

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

## NodeGroup

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| name | `string` |  | Nodegroup name.<br> |
| labels | `map[string]string` | [] | Labels that you want to be propagated to each node in the nodegroup.<br><br>Each label should meet one of the following criterias to propagate to each of the nodes :<br><br>  1. Has node-role.kubernetes.io as prefix.<br>  2. Belongs to node-restriction.kubernetes.io domain.<br>  3. Belongs to node.cluster.x-k8s.io domain.<br><br>REFER : https://cluster-api.sigs.k8s.io/developer/architecture/controllers/metadata-propagation#machine.<br> |
| taints | []`k8s.io/api/core/v1.Taint` | [] | Taints that you want to be propagated to each node in the nodegroup.<br> |

## OIDCConfig

<p>OIDCConfig is the typed kube-apiserver OIDC configuration.

Required fields (IssuerURL + ClientID) must be present when the
block is set; the rest carry sensible defaults. The IssuerURL is
also probed at bootstrap time (see parser.ValidateOIDCDiscovery)
so an unreachable / mistyped issuer fails fast — before we
provision infrastructure.</p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| issuerUrl | `string` |  | IssuerURL is the Keycloak realm URL (e.g.<br>https://keycloak.<vpn-server>/realms/clusters). kube-apiserver<br>validates JWTs against this issuer's JWKS.<br> |
| clientId | `string` |  | ClientID is the per-cluster OIDC client created in Keycloak<br>(e.g. kubernetes-staging). Must match the `aud` claim in<br>tokens kube-apiserver should accept.<br> |
| usernameClaim | `string` | email | UsernameClaim is the JWT claim kube-apiserver maps to the<br>user's identity. Defaults to "email" — what the architecture<br>doc recommends — but can be overridden per Keycloak setup.<br> |
| groupsClaim | `string` | groups | GroupsClaim is the JWT claim kube-apiserver reads to<br>populate the user's groups for RBAC. Defaults to "groups".<br> |
| usernamePrefix | `string` |  | UsernamePrefix is prepended to usernames extracted from the<br>token (e.g. "oidc:"). Empty by default — useful when you<br>want to avoid collisions with non-OIDC users in RBAC bindings.<br> |
| groupsPrefix | `string` |  | GroupsPrefix is prepended to groups extracted from the token<br>(e.g. "oidc:"). Empty by default.<br> |
| caBundlePath | `string` |  | CABundlePath is an absolute host path to a PEM file<br>containing the CA that signed the issuer's TLS certificate.<br>Set this only when the issuer's cert is not chainable to a<br>publicly-trusted CA. When set, the parser reads the file<br>at config-render time and embeds its contents inline in the<br>AuthenticationConfiguration YAML.<br> |

## ObmondoConfig

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| customerID | `string` |  |  |
| monitoring | `bool` |  |  |
| certPath | `string` |  | Path to the mTLS client cert issued by Obmondo. Required when<br>Monitoring is true — kubeaid-agent uses it to authenticate to the<br>Obmondo API, and kube-prometheus's Alertmanager uses it to push<br>alerts to Obmondo's alert-receiver endpoint.<br> |
| keyPath | `string` |  | Path to the private key paired with CertPath. Required when<br>Monitoring is true.<br> |
| teleportAgent | `bool` |  | TeleportAgent gates the teleport-kube-agent ArgoCD App. Defaults to<br>true when Monitoring is true. Set explicitly to false to skip it —<br>e.g. test environments that don't have a valid join token, or<br>clusters that'll use the upcoming Netbird-backed gateway instead.<br> |

## ObmondoCredentials

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| teleportAuthToken | `string` |  | TeleportAuthToken is the join token teleport-kube-agent uses to<br>register with the Teleport cluster. Required when<br>obmondo.monitoring is true and obmondo.teleportAgent isn't false.<br> |

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
| obmondo | [`ObmondoCredentials`](#obmondocredentials) |  |  |
| keycloak | [`KeycloakCredentials`](#keycloakcredentials) |  |  |
| netbird | [`NetBirdCredentials`](#netbirdcredentials) |  |  |

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
| subnetCIDRBlock | `string` |  |  |

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