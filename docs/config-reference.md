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
- [KubeAidForkConfig](#kubeaidforkconfig)
- [KubePrometheusConfig](#kubeprometheusconfig)
- [KubeaidConfigForkConfig](#kubeaidconfigforkconfig)
- [LocalConfig](#localconfig)
- [NodeGroup](#nodegroup)
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
| privateKeyFilePath | `string` |  |  |

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
| apiServer | [`APIServerConfig`](#apiserverconfig) |  | Configuration options for the Kubernetes API server.<br> |
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
| imagePullPolicy | `string` | IfNotPresent | Image pull policy for the KubeAid Core container image.<br>Valid values: Always, IfNotPresent, Never.<br> |
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
| useSSHAgent | `bool` |  | Or, make KubeAid CLI use the SSH Agent.<br>So, you (the one who runs KubeAid CLI) can use your YubiKey.<br> |
| knownHosts | []`string` |  | Additional SSH known hosts.<br>Merged with known hosts of common Git repo hosting providers (like Azure DevOps, GitLab etc.)<br> |
| privateKeyFilePath | `string` |  |  |

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
| vpnCluster | [`HCloudVPNClusterConfig`](#hcloudvpnclusterconfig) |  |  |

## HetznerControlPlane

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| hcloud | [`HCloudControlPlane`](#hcloudcontrolplane) |  |  |
| bareMetal | [`HetznerBareMetalControlPlane`](#hetznerbaremetalcontrolplane) |  |  |
| regions | []`string` |  |  |

## HetznerCredentials

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| apiToken | `string` |  |  |
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
| privateKeyFilePath | `string` |  |  |

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
| imagePath | `string` | /root/.oldroot/nfs/images/Ubuntu-2404-noble-amd64-base.tar.gz |  |
| vg0 | [`VG0Config`](#vg0config) |  |  |

## KubeAidForkConfig

<p>KubeAid repository specific details.</p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| url | `string` |  | KubeAid repository SSH URL.<br> |
| version | `string` |  | KubeAid tag.<br> |

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
| privateKeyFilePath | `string` |  |  |

## SSHKeyPairConfig

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| privateKeyFilePath | `string` |  |  |

## SecretsConfig

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| aws | [`AWSCredentials`](#awscredentials) |  |  |
| azure | [`AzureCredentials`](#azurecredentials) |  |  |
| hetzner | [`HetznerCredentials`](#hetznercredentials) |  |  |
| obmondo | [`ObmondoCredentials`](#obmondocredentials) |  |  |

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