# Configuration Reference
- [AADApplication](#aadapplication)
- [AMIConfig](#amiconfig)
- [APIServerConfig](#apiserverconfig)
- [AWSAutoScalableNodeGroup](#awsautoscalablenodegroup)
- [AWSConfig](#awsconfig)
- [AWSControlPlane](#awscontrolplane)
- [AWSCredentials](#awscredentials)
- [ArgoCDCredentials](#argocdcredentials)
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
- [CEPHConfig](#cephconfig)
- [CanonicalUbuntuImage](#canonicalubuntuimage)
- [CloudConfig](#cloudconfig)
- [ClusterConfig](#clusterconfig)
- [DisasterRecoveryConfig](#disasterrecoveryconfig)
- [FileConfig](#fileconfig)
- [ForksConfig](#forksconfig)
- [GeneralConfig](#generalconfig)
- [GitConfig](#gitconfig)
- [GitCredentials](#gitcredentials)
- [HCloudAutoScalableNodeGroup](#hcloudautoscalablenodegroup)
- [HCloudControlPlane](#hcloudcontrolplane)
- [HCloudControlPlaneLoadBalancer](#hcloudcontrolplaneloadbalancer)
- [HetznerBareMetalConfig](#hetznerbaremetalconfig)
- [HetznerBareMetalControlPlane](#hetznerbaremetalcontrolplane)
- [HetznerBareMetalControlPlaneEndpoint](#hetznerbaremetalcontrolplaneendpoint)
- [HetznerBareMetalHost](#hetznerbaremetalhost)
- [HetznerBareMetalNodeGroup](#hetznerbaremetalnodegroup)
- [HetznerBareMetalSSHKeyPair](#hetznerbaremetalsshkeypair)
- [HetznerConfig](#hetznerconfig)
- [HetznerControlPlane](#hetznercontrolplane)
- [HetznerCredentials](#hetznercredentials)
- [HetznerHCloudConfig](#hetznerhcloudconfig)
- [HetznerNodeGroups](#hetznernodegroups)
- [HetznerRobotCredentials](#hetznerrobotcredentials)
- [HostPathMountConfig](#hostpathmountconfig)
- [InstallImageConfig](#installimageconfig)
- [KubeAidForkConfig](#kubeaidforkconfig)
- [KubePrometheusConfig](#kubeprometheusconfig)
- [KubeaidConfigForkConfig](#kubeaidconfigforkconfig)
- [LocalConfig](#localconfig)
- [NodeGroup](#nodegroup)
- [ObmondoConfig](#obmondoconfig)
- [SSHKeyPairConfig](#sshkeypairconfig)
- [SSHPrivateKeyConfig](#sshprivatekeyconfig)
- [SecretsConfig](#secretsconfig)
- [UserConfig](#userconfig)
- [VG0Config](#vg0config)
- [VSwitchConfig](#vswitchconfig)
- [WorkloadIdentity](#workloadidentity)

## AADApplication

&lt;p&gt;&lt;/p&gt;

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| principalID | `string` |  |  |

## AMIConfig

&lt;p&gt;&lt;/p&gt;

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| id | `string` |  |  |

## APIServerConfig

&lt;p&gt;REFER : https://github.com/kubernetes-sigs/cluster-api/blob/main/controlplane/kubeadm/config/crd/bases/controlplane.cluster.x-k8s.io_kubeadmcontrolplanes.yaml.

NOTE : Generally, refer to the KubeadmControlPlane CRD instead of the corresponding GoLang
       source types linked below. There are some configuration options which appear in the
       corresponding GoLang source type, but not in the CRD. If you set those fields, then
       they get removed by the Kubeadm control-plane provider. This causes the capi-cluster
       ArgoCD App to always be in an OutOfSync state, resulting to KubeAid CLI not making any
       progress!&lt;/p&gt;

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| extraArgs | `map[string]string` | {} |  |
| extraVolumes | `[]HostPathMountConfig` | [] |  |
| files | `[]FileConfig` | [] |  |

## AWSAutoScalableNodeGroup

&lt;p&gt;&lt;/p&gt;

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| ami | `AMIConfig` |  |  |
| instanceType | `string` |  |  |
| rootVolumeSize | `uint32` |  |  |
| sshKeyName | `string` |  |  |
| name | `string` |  | Nodegroup name.
 |
| labels | `map[string]string` | [] | Labels that you want to be propagated to each node in the nodegroup.

Each label should meet one of the following criterias to propagate to each of the nodes :

  1. Has node-role.kubernetes.io as prefix.
  2. Belongs to node-restriction.kubernetes.io domain.
  3. Belongs to node.cluster.x-k8s.io domain.

REFER : https://cluster-api.sigs.k8s.io/developer/architecture/controllers/metadata-propagation#machine.
 |
| taints | `[]k8s.io/api/core/v1.Taint` | [] | Taints that you want to be propagated to each node in the nodegroup.
 |
| minSize | `uint` |  | Minimum number of replicas in the nodegroup.
 |
| maxSize | `uint` |  | Maximum number of replicas in the nodegroup.
 |

## AWSConfig

&lt;p&gt;&lt;/p&gt;

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| region | `string` |  |  |
| sshKeyName | `string` |  |  |
| vpcID | `string` |  |  |
| bastionEnabled | `bool` | True |  |
| controlPlane | `AWSControlPlane` |  |  |
| nodeGroups | `[]AWSAutoScalableNodeGroup` |  |  |

## AWSControlPlane

&lt;p&gt;&lt;/p&gt;

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| loadBalancerScheme | `string` | internet-facing |  |
| replicas | `uint32` |  |  |
| instanceType | `string` |  |  |
| ami | `AMIConfig` |  |  |

## AWSCredentials

&lt;p&gt;&lt;/p&gt;

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| accessKeyID | `string` |  |  |
| secretAccessKey | `string` |  |  |
| sessionToken | `string` |  |  |

## ArgoCDCredentials

&lt;p&gt;&lt;/p&gt;

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| git | `GitCredentials` |  | Git specific credentials, used by ArgoCD to watch the KubeAid and KubeAid Config repositories.

NOTE : We enforce the user, not to make ArgoCD use SSH authentication against the Git server,
       since : that way, ArgoCD gets both read and write permissions.
 |

## AutoScalableNodeGroup

&lt;p&gt;&lt;/p&gt;

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| minSize | `uint` |  | Minimum number of replicas in the nodegroup.
 |
| maxSize | `uint` |  | Maximum number of replicas in the nodegroup.
 |
| name | `string` |  | Nodegroup name.
 |
| labels | `map[string]string` | [] | Labels that you want to be propagated to each node in the nodegroup.

Each label should meet one of the following criterias to propagate to each of the nodes :

  1. Has node-role.kubernetes.io as prefix.
  2. Belongs to node-restriction.kubernetes.io domain.
  3. Belongs to node.cluster.x-k8s.io domain.

REFER : https://cluster-api.sigs.k8s.io/developer/architecture/controllers/metadata-propagation#machine.
 |
| taints | `[]k8s.io/api/core/v1.Taint` | [] | Taints that you want to be propagated to each node in the nodegroup.
 |

## AzureAutoScalableNodeGroup

&lt;p&gt;&lt;/p&gt;

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| vmSize | `string` |  |  |
| diskSizeGB | `uint32` |  |  |
| minSize | `uint` |  | Minimum number of replicas in the nodegroup.
 |
| maxSize | `uint` |  | Maximum number of replicas in the nodegroup.
 |
| name | `string` |  | Nodegroup name.
 |
| labels | `map[string]string` | [] | Labels that you want to be propagated to each node in the nodegroup.

Each label should meet one of the following criterias to propagate to each of the nodes :

  1. Has node-role.kubernetes.io as prefix.
  2. Belongs to node-restriction.kubernetes.io domain.
  3. Belongs to node.cluster.x-k8s.io domain.

REFER : https://cluster-api.sigs.k8s.io/developer/architecture/controllers/metadata-propagation#machine.
 |
| taints | `[]k8s.io/api/core/v1.Taint` | [] | Taints that you want to be propagated to each node in the nodegroup.
 |

## AzureConfig

&lt;p&gt;&lt;/p&gt;

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| tenantID | `string` |  |  |
| subscriptionID | `string` |  |  |
| aadApplication | `AADApplication` |  |  |
| location | `string` |  |  |
| storageAccount | `string` |  |  |
| workloadIdentity | `WorkloadIdentity` |  |  |
| sshPublicKey | `string` |  |  |
| canonicalUbuntuImage | `CanonicalUbuntuImage` |  |  |
| controlPlane | `AzureControlPlane` |  |  |
| nodeGroups | `[]AzureAutoScalableNodeGroup` |  |  |

## AzureControlPlane

&lt;p&gt;&lt;/p&gt;

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| loadBalancerType | `string` | Public |  |
| diskSizeGB | `uint32` |  |  |
| vmSize | `string` |  |  |
| replicas | `uint32` |  |  |

## AzureCredentials

&lt;p&gt;&lt;/p&gt;

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| clientID | `string` |  |  |
| clientSecret | `string` |  |  |

## BareMetalConfig

&lt;p&gt;&lt;/p&gt;

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| ssh | `BareMetalSSHConfig` |  |  |
| controlPlane | `BareMetalControlPlane` |  |  |
| nodeGroups | `[]BareMetalNodeGroup` |  |  |

## BareMetalControlPlane

&lt;p&gt;&lt;/p&gt;

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| endpoint | `BareMetalControlPlaneEndpoint` |  |  |
| hosts | `[]BareMetalHost` |  |  |

## BareMetalControlPlaneEndpoint

&lt;p&gt;&lt;/p&gt;

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| host | `string` |  |  |
| port | `uint` | 6443 |  |

## BareMetalHost

&lt;p&gt;&lt;/p&gt;

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| publicAddress | `string` |  |  |
| privateAddress | `string` |  |  |
| ssh | `BareMetalSSHConfig` |  |  |

## BareMetalNodeGroup

&lt;p&gt;&lt;/p&gt;

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| hosts | `[]BareMetalHost` |  |  |
| name | `string` |  | Nodegroup name.
 |
| labels | `map[string]string` | [] | Labels that you want to be propagated to each node in the nodegroup.

Each label should meet one of the following criterias to propagate to each of the nodes :

  1. Has node-role.kubernetes.io as prefix.
  2. Belongs to node-restriction.kubernetes.io domain.
  3. Belongs to node.cluster.x-k8s.io domain.

REFER : https://cluster-api.sigs.k8s.io/developer/architecture/controllers/metadata-propagation#machine.
 |
| taints | `[]k8s.io/api/core/v1.Taint` | [] | Taints that you want to be propagated to each node in the nodegroup.
 |

## BareMetalSSHConfig

&lt;p&gt;&lt;/p&gt;

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| port | `uint` | 22 |  |
| privateKey | `SSHPrivateKeyConfig` |  |  |

## CEPHConfig

&lt;p&gt;&lt;/p&gt;

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| deviceFilter | `string` |  |  |

## CanonicalUbuntuImage

&lt;p&gt;&lt;/p&gt;

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| offer | `string` |  |  |
| sku | `string` |  |  |

## CloudConfig

&lt;p&gt;&lt;/p&gt;

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| aws | `AWSConfig` |  |  |
| azure | `AzureConfig` |  |  |
| hetzner | `HetznerConfig` |  |  |
| bare-metal | `BareMetalConfig` |  |  |
| local | `LocalConfig` |  |  |
| disasterRecovery | `DisasterRecoveryConfig` |  |  |

## ClusterConfig

&lt;p&gt;&lt;/p&gt;

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| name | `string` |  | Name of the Kubernetes cluster.

We don&#39;t allow using dots in the cluster name, since it can cause issues with tools like
ClusterAPI and Cilium : which use the cluster name to generate other configurations.
 |
| k8sVersion | `string` |  | Kubernetes version (&gt;= 1.30.0).
 |
| enableAuditLogging | `bool` | True | Whether you would like to enable Kubernetes Audit Logging out of the box.
Suitable Kubernetes API configurations will be done for you automatically. And they can be
changed using the apiSever struct field.
 |
| apiServer | `APIServerConfig` |  | Configuration options for the Kubernetes API server.
 |
| additionalUsers | `[]UserConfig` |  | Other than the root user, addtional users that you would like to be created in each node.
NOTE : Currently, we can&#39;t register additional SSH key-pairs against the root user.
 |

## DisasterRecoveryConfig

&lt;p&gt;&lt;/p&gt;

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| veleroBackupsBucketName | `string` |  |  |
| sealedSecretsBackupsBucketName | `string` |  |  |

## FileConfig

&lt;p&gt;REFER : &#34;sigs.k8s.io/cluster-api/bootstrap/kubeadm/api/v1beta1&#34;.File.&lt;/p&gt;

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| path | `string` |  |  |
| content | `string` |  |  |

## ForksConfig

&lt;p&gt;KubeAid and KubeAid Config repository specific details.
We require the KubeAid and KubeAid Config repositories to be hosted in the same Git server.&lt;/p&gt;

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| kubeaid | `KubeAidForkConfig` |  | KubeAid repository specific details.
 |
| kubeaidConfig | `KubeaidConfigForkConfig` |  | KubeAid Config repository specific details.
 |

## GeneralConfig

&lt;p&gt;Non secret configuration options.&lt;/p&gt;

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| git | `GitConfig` |  | Git server specific details.
 |
| forkURLs | `ForksConfig` |  | KubeAid and KubeAid Config repository specific details.
The KubeAid and KubeAid Config repositories must be hosted in the same Git server.
 |
| cluster | `ClusterConfig` |  | Kubernetes specific details.
 |
| cloud | `CloudConfig` |  | Cloud provider specific details.
 |
| kubePrometheus | `KubePrometheusConfig` |  | Kube Prometheus installation specific details.
 |
| obmondo | `ObmondoConfig` |  | Obmondo customer specific details.
 |

## GitConfig

&lt;p&gt;Git specific details, used by KubeAid CLI,
to clone repositories from and push changes to the Git server.
We enforce the user to use SSH, for authenticating to the Git server.&lt;/p&gt;

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| caBundlePath | `string` |  |  |
| sshUsername | `string` | git | SSH username.
 |
| useSSHAgent | `bool` |  | Or, make KubeAid CLI use the SSH Agent.
So, you (the one who runs KubeAid CLI) can use your YubiKey.
 |
| privateKeyFilePath | `string` |  |  |

## GitCredentials

&lt;p&gt;&lt;/p&gt;

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| username | `string` |  |  |
| password | `string` |  |  |

## HCloudAutoScalableNodeGroup

&lt;p&gt;Details about (autoscalable) node-groups in HCloud.&lt;/p&gt;

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| machineType | `string` |  | HCloud machine type.
You can browse all available HCloud machine types here : https://hetzner.com/cloud.
 |
| name | `string` |  | Nodegroup name.
 |
| labels | `map[string]string` | [] | Labels that you want to be propagated to each node in the nodegroup.

Each label should meet one of the following criterias to propagate to each of the nodes :

  1. Has node-role.kubernetes.io as prefix.
  2. Belongs to node-restriction.kubernetes.io domain.
  3. Belongs to node.cluster.x-k8s.io domain.

REFER : https://cluster-api.sigs.k8s.io/developer/architecture/controllers/metadata-propagation#machine.
 |
| taints | `[]k8s.io/api/core/v1.Taint` | [] | Taints that you want to be propagated to each node in the nodegroup.
 |
| minSize | `uint` |  | Minimum number of replicas in the nodegroup.
 |
| maxSize | `uint` |  | Maximum number of replicas in the nodegroup.
 |

## HCloudControlPlane

&lt;p&gt;&lt;/p&gt;

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| machineType | `string` |  |  |
| replicas | `uint` |  |  |
| loadBalancer | `HCloudControlPlaneLoadBalancer` |  |  |

## HCloudControlPlaneLoadBalancer

&lt;p&gt;&lt;/p&gt;

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| enabled | `bool` |  |  |
| region | `string` |  |  |

## HetznerBareMetalConfig

&lt;p&gt;&lt;/p&gt;

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| wipeDisks | `bool` | false |  |
| installImage | `InstallImageConfig` |  |  |
| sshKeyPair | `HetznerBareMetalSSHKeyPair` |  |  |
| diskLayoutSetupCommands | `string` |  |  |
| ceph | `CEPHConfig` |  |  |

## HetznerBareMetalControlPlane

&lt;p&gt;&lt;/p&gt;

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| endpoint | `HetznerBareMetalControlPlaneEndpoint` |  |  |
| bareMetalHosts | `[]HetznerBareMetalHost` |  |  |
| diskLayoutSetupCommands | `string` |  |  |

## HetznerBareMetalControlPlaneEndpoint

&lt;p&gt;&lt;/p&gt;

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| isFailoverIP | `bool` |  |  |
| host | `string` |  |  |

## HetznerBareMetalHost

&lt;p&gt;&lt;/p&gt;

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| serverID | `string` |  |  |
| wwns | `[]string` |  |  |

## HetznerBareMetalNodeGroup

&lt;p&gt;&lt;/p&gt;

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| bareMetalHosts | `[]HetznerBareMetalHost` |  |  |
| diskLayoutSetupCommands | `string` |  |  |
| name | `string` |  | Nodegroup name.
 |
| labels | `map[string]string` | [] | Labels that you want to be propagated to each node in the nodegroup.

Each label should meet one of the following criterias to propagate to each of the nodes :

  1. Has node-role.kubernetes.io as prefix.
  2. Belongs to node-restriction.kubernetes.io domain.
  3. Belongs to node.cluster.x-k8s.io domain.

REFER : https://cluster-api.sigs.k8s.io/developer/architecture/controllers/metadata-propagation#machine.
 |
| taints | `[]k8s.io/api/core/v1.Taint` | [] | Taints that you want to be propagated to each node in the nodegroup.
 |

## HetznerBareMetalSSHKeyPair

&lt;p&gt;&lt;/p&gt;

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| name | `string` |  |  |
| publicKeyFilePath | `string` |  |  |
| privateKeyFilePath | `string` |  |  |

## HetznerConfig

&lt;p&gt;&lt;/p&gt;

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| mode | `string` | hcloud | The Hetzner mode to use :

  1. hcloud : Both the control-plane and the nodegroups will be in HCloud.

  2. bare-metal : Both the control-plane and the nodegroups will be in Hetzner Bare Metal.

  3. hybrid : The control-plane will be in HCloud, and each node-group can be either in
              HCloud or Hetzner Bare Metal.
 |
| vswitch | `VSwitchConfig` |  |  |
| hcloud | `HetznerHCloudConfig` |  |  |
| bareMetal | `HetznerBareMetalConfig` |  |  |
| controlPlane | `HetznerControlPlane` |  |  |
| nodeGroups | `HetznerNodeGroups` |  | Details about node-groups in Hetzner.
 |

## HetznerControlPlane

&lt;p&gt;&lt;/p&gt;

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| hcloud | `HCloudControlPlane` |  |  |
| bareMetal | `HetznerBareMetalControlPlane` |  |  |
| regions | `[]string` |  |  |

## HetznerCredentials

&lt;p&gt;&lt;/p&gt;

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| apiToken | `string` |  |  |
| robot | `HetznerRobotCredentials` |  |  |

## HetznerHCloudConfig

&lt;p&gt;&lt;/p&gt;

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| zone | `string` |  |  |
| imageName | `string` | ubuntu-24.04 |  |
| sshKeyPairName | `string` |  |  |

## HetznerNodeGroups

&lt;p&gt;Details about node-groups in Hetzner.&lt;/p&gt;

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| hcloud | `[]HCloudAutoScalableNodeGroup` |  | Details about node-groups in HCloud.
 |
| bareMetal | `[]HetznerBareMetalNodeGroup` |  | Details about node-groups in Hetzner Bare Metal.
 |

## HetznerRobotCredentials

&lt;p&gt;&lt;/p&gt;

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| user | `string` |  |  |
| password | `string` |  |  |

## HostPathMountConfig

&lt;p&gt;REFER : &#34;sigs.k8s.io/cluster-api/bootstrap/kubeadm/api/v1beta1&#34;.HostPathMount&lt;/p&gt;

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| name | `string` |  |  |
| hostPath | `string` |  |  |
| mountPath | `string` |  |  |
| pathType | `k8s.io/api/core/v1.HostPathType` |  |  |
| readOnly | `bool` | true | Whether the mount should be read-only.
 |

## InstallImageConfig

&lt;p&gt;&lt;/p&gt;

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| imagePath | `string` | /root/.oldroot/nfs/images/Ubuntu-2404-noble-amd64-base.tar.gz |  |
| vg0 | `VG0Config` |  |  |

## KubeAidForkConfig

&lt;p&gt;KubeAid repository specific details.&lt;/p&gt;

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| url | `string` |  | KubeAid repository SSH URL.
 |
| version | `string` |  | KubeAid tag.
 |

## KubePrometheusConfig

&lt;p&gt;&lt;/p&gt;

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| version | `string` | v0.15.0 |  |
| grafanaURL | `string` |  |  |

## KubeaidConfigForkConfig

&lt;p&gt;KubeAid Config repository specific details.&lt;/p&gt;

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| url | `string` |  | KubeAid Config repository SSH URL.
 |
| directory | `string` |  | Name of the directory inside your KubeAid Config repository&#39;s k8s folder, where the KubeAid
Config files for this cluster will be contained.

When not specified, the directory name will default to the cluster name.

So, suppose your cluster name is &#39;staging&#39;. Then, the directory name will default to
&#39;staging&#39;. Or you can customize it to something like &#39;staging.qa&#39;.
 |

## LocalConfig

&lt;p&gt;Local specific.&lt;/p&gt;

| Field | Type | Default | Description |
|-------|------|---------|-------------|

## NodeGroup

&lt;p&gt;&lt;/p&gt;

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| name | `string` |  | Nodegroup name.
 |
| labels | `map[string]string` | [] | Labels that you want to be propagated to each node in the nodegroup.

Each label should meet one of the following criterias to propagate to each of the nodes :

  1. Has node-role.kubernetes.io as prefix.
  2. Belongs to node-restriction.kubernetes.io domain.
  3. Belongs to node.cluster.x-k8s.io domain.

REFER : https://cluster-api.sigs.k8s.io/developer/architecture/controllers/metadata-propagation#machine.
 |
| taints | `[]k8s.io/api/core/v1.Taint` | [] | Taints that you want to be propagated to each node in the nodegroup.
 |

## ObmondoConfig

&lt;p&gt;&lt;/p&gt;

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| customerID | `string` |  |  |
| monitoring | `bool` |  |  |

## SSHKeyPairConfig

&lt;p&gt;&lt;/p&gt;

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| publicKeyFilePath | `string` |  |  |
| privateKeyFilePath | `string` |  |  |

## SSHPrivateKeyConfig

&lt;p&gt;&lt;/p&gt;

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| privateKeyFilePath | `string` |  |  |

## SecretsConfig

&lt;p&gt;&lt;/p&gt;

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| argoCD | `ArgoCDCredentials` |  |  |
| aws | `AWSCredentials` |  |  |
| azure | `AzureCredentials` |  |  |
| hetzner | `HetznerCredentials` |  |  |

## UserConfig

&lt;p&gt;&lt;/p&gt;

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| name | `string` |  |  |
| sshPublicKey | `string` |  |  |

## VG0Config

&lt;p&gt;&lt;/p&gt;

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| size | `string` | 25G |  |
| rootVolumeSize | `string` | 10G |  |

## VSwitchConfig

&lt;p&gt;&lt;/p&gt;

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| vlanID | `int` |  |  |
| name | `string` |  |  |

## WorkloadIdentity

&lt;p&gt;&lt;/p&gt;

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| openIDProviderSSHKeyPair | `SSHKeyPairConfig` |  |  |
