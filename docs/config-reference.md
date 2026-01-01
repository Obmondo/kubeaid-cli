# Configuration Reference
- [AADApplication](#aadapplication)
- [AMIConfig](#amiconfig)
- [APIServerConfig](#apiserverconfig)
- [AWSAutoScalableNodeGroup](#awsautoscalablenodegroup)
- [AWSConfig](#awsconfig)
- [AWSControlPlane](#awscontrolplane)
- [AWSCredentials](#awscredentials)
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
- [GitUsernameAndPassword](#gitusernameandpassword)
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

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| PrincipalID | `string` |  |  |

## AMIConfig

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| ID | `string` |  |  |

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
| ExtraArgs | `map[string]string` | {} |  |
| ExtraVolumes | `[]HostPathMountConfig` | [] |  |
| Files | `[]FileConfig` | [] |  |

## AWSAutoScalableNodeGroup

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| AMI | `AMIConfig` |  |  |
| InstanceType | `string` |  |  |
| RootVolumeSize | `uint32` |  |  |
| SSHKeyName | `string` |  |  |
| Name | `string` |  | Nodegroup name.<br> |
| Labels | `map[string]string` | [] | Labels that you want to be propagated to each node in the nodegroup.<br><br>Each label should meet one of the following criterias to propagate to each of the nodes :<br><br>  1. Has node-role.kubernetes.io as prefix.<br>  2. Belongs to node-restriction.kubernetes.io domain.<br>  3. Belongs to node.cluster.x-k8s.io domain.<br><br>REFER : https://cluster-api.sigs.k8s.io/developer/architecture/controllers/metadata-propagation#machine.<br> |
| Taints | `[]k8s.io/api/core/v1.Taint` | [] | Taints that you want to be propagated to each node in the nodegroup.<br> |
| MinSize | `uint` |  | Minimum number of replicas in the nodegroup.<br> |
| Maxsize | `uint` |  | Maximum number of replicas in the nodegroup.<br> |

## AWSConfig

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| Region | `string` |  |  |
| SSHKeyName | `string` |  |  |
| VPCID | `string` |  |  |
| BastionEnabled | `bool` | True |  |
| ControlPlane | `AWSControlPlane` |  |  |
| NodeGroups | `[]AWSAutoScalableNodeGroup` |  |  |

## AWSControlPlane

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| LoadBalancerScheme | `string` | internet-facing |  |
| Replicas | `uint32` |  |  |
| InstanceType | `string` |  |  |
| AMI | `AMIConfig` |  |  |

## AWSCredentials

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| AWSAccessKeyID | `string` |  |  |
| AWSSecretAccessKey | `string` |  |  |
| AWSSessionToken | `string` |  |  |

## AutoScalableNodeGroup

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| MinSize | `uint` |  | Minimum number of replicas in the nodegroup.<br> |
| Maxsize | `uint` |  | Maximum number of replicas in the nodegroup.<br> |
| Name | `string` |  | Nodegroup name.<br> |
| Labels | `map[string]string` | [] | Labels that you want to be propagated to each node in the nodegroup.<br><br>Each label should meet one of the following criterias to propagate to each of the nodes :<br><br>  1. Has node-role.kubernetes.io as prefix.<br>  2. Belongs to node-restriction.kubernetes.io domain.<br>  3. Belongs to node.cluster.x-k8s.io domain.<br><br>REFER : https://cluster-api.sigs.k8s.io/developer/architecture/controllers/metadata-propagation#machine.<br> |
| Taints | `[]k8s.io/api/core/v1.Taint` | [] | Taints that you want to be propagated to each node in the nodegroup.<br> |

## AzureAutoScalableNodeGroup

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| VMSize | `string` |  |  |
| DiskSizeGB | `uint32` |  |  |
| MinSize | `uint` |  | Minimum number of replicas in the nodegroup.<br> |
| Maxsize | `uint` |  | Maximum number of replicas in the nodegroup.<br> |
| Name | `string` |  | Nodegroup name.<br> |
| Labels | `map[string]string` | [] | Labels that you want to be propagated to each node in the nodegroup.<br><br>Each label should meet one of the following criterias to propagate to each of the nodes :<br><br>  1. Has node-role.kubernetes.io as prefix.<br>  2. Belongs to node-restriction.kubernetes.io domain.<br>  3. Belongs to node.cluster.x-k8s.io domain.<br><br>REFER : https://cluster-api.sigs.k8s.io/developer/architecture/controllers/metadata-propagation#machine.<br> |
| Taints | `[]k8s.io/api/core/v1.Taint` | [] | Taints that you want to be propagated to each node in the nodegroup.<br> |

## AzureConfig

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| TenantID | `string` |  |  |
| SubscriptionID | `string` |  |  |
| AADApplication | `AADApplication` |  |  |
| Location | `string` |  |  |
| StorageAccount | `string` |  |  |
| WorkloadIdentity | `WorkloadIdentity` |  |  |
| SSHPublicKey | `string` |  |  |
| CanonicalUbuntuImage | `CanonicalUbuntuImage` |  |  |
| ControlPlane | `AzureControlPlane` |  |  |
| NodeGroups | `[]AzureAutoScalableNodeGroup` |  |  |

## AzureControlPlane

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| LoadBalancerType | `string` | Public |  |
| DiskSizeGB | `uint32` |  |  |
| VMSize | `string` |  |  |
| Replicas | `uint32` |  |  |

## AzureCredentials

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| ClientID | `string` |  |  |
| ClientSecret | `string` |  |  |

## BareMetalConfig

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| SSH | `BareMetalSSHConfig` |  |  |
| ControlPlane | `BareMetalControlPlane` |  |  |
| NodeGroups | `[]BareMetalNodeGroup` |  |  |

## BareMetalControlPlane

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| Endpoint | `BareMetalControlPlaneEndpoint` |  |  |
| Hosts | `[]BareMetalHost` |  |  |

## BareMetalControlPlaneEndpoint

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| Host | `string` |  |  |
| Port | `uint` | 6443 |  |

## BareMetalHost

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| PublicAddress | `string` |  |  |
| PrivateAddress | `string` |  |  |
| SSH | `BareMetalSSHConfig` |  |  |

## BareMetalNodeGroup

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| Hosts | `[]BareMetalHost` |  |  |
| Name | `string` |  | Nodegroup name.<br> |
| Labels | `map[string]string` | [] | Labels that you want to be propagated to each node in the nodegroup.<br><br>Each label should meet one of the following criterias to propagate to each of the nodes :<br><br>  1. Has node-role.kubernetes.io as prefix.<br>  2. Belongs to node-restriction.kubernetes.io domain.<br>  3. Belongs to node.cluster.x-k8s.io domain.<br><br>REFER : https://cluster-api.sigs.k8s.io/developer/architecture/controllers/metadata-propagation#machine.<br> |
| Taints | `[]k8s.io/api/core/v1.Taint` | [] | Taints that you want to be propagated to each node in the nodegroup.<br> |

## BareMetalSSHConfig

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| Port | `uint` | 22 |  |
| PrivateKey | `SSHPrivateKeyConfig` |  |  |

## CEPHConfig

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| DeviceFilter | `string` |  |  |

## CanonicalUbuntuImage

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| Offer | `string` |  |  |
| SKU | `string` |  |  |

## CloudConfig

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| AWS | `AWSConfig` |  |  |
| Azure | `AzureConfig` |  |  |
| Hetzner | `HetznerConfig` |  |  |
| BareMetal | `BareMetalConfig` |  |  |
| Local | `LocalConfig` |  |  |
| DisasterRecovery | `DisasterRecoveryConfig` |  |  |

## ClusterConfig

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| Name | `string` |  | Name of the Kubernetes cluster.<br><br>We don't allow using dots in the cluster name, since it can cause issues with tools like<br>ClusterAPI and Cilium : which use the cluster name to generate other configurations.<br> |
| K8sVersion | `string` |  | Kubernetes version ( >= 1.30.0).<br> |
| EnableAuditLogging | `bool` | True | Whether you would like to enable Kubernetes Audit Logging out of the box.<br>Suitable Kubernetes API configurations will be done for you automatically. And they can be<br>changed using the apiSever struct field.<br> |
| APIServer | `APIServerConfig` |  | Configuration options for the Kubernetes API server.<br> |
| AdditionalUsers | `[]UserConfig` |  | Other than the root user, addtional users that you would like to be created in each node.<br>NOTE : Currently, we can't register additional SSH key-pairs against the root user.<br> |

## DisasterRecoveryConfig

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| VeleroBackupsBucketName | `string` |  |  |
| SealedSecretsBackupsBucketName | `string` |  |  |

## FileConfig

<p>REFER : "sigs.k8s.io/cluster-api/bootstrap/kubeadm/api/v1beta1".File.</p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| Path | `string` |  |  |
| Content | `string` |  |  |

## ForksConfig

<p>KubeAid and KubeAid Config repository speicific details.
For now, we require the KubeAid and KubeAid Config repositories to be hosted in the same
Git server.</p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| KubeaidFork | `KubeAidForkConfig` |  | KubeAid repository specific details.<br> |
| KubeaidConfigFork | `KubeaidConfigForkConfig` |  | KubeAid Config repository specific details.<br> |

## GeneralConfig

<p>Non secret configuration options.</p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| Git | `GitConfig` |  | Git server spcific details.<br> |
| Forks | `ForksConfig` |  | KubeAid and KubeAid Config repository specific details.<br>The KubeAid and KubeAid Config repositories must be hosted in the same Git server.<br> |
| Cluster | `ClusterConfig` |  | Kubernetes specific details.<br> |
| Cloud | `CloudConfig` |  | Cloud provider specific details.<br> |
| KubePrometheus | `KubePrometheusConfig` |  | Kube Prometheus installation specific details.<br> |
| Obmondo | `ObmondoConfig` |  | Obmondo customer specific details.<br> |

## GitConfig

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| CABundlePath | `string` |  |  |
| UseSSHAgentAuth | `bool` |  |  |
| PrivateKeyFilePath | `string` |  |  |

## GitCredentials

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| Username | `string` |  |  |
| Password | `string` |  |  |

## GitUsernameAndPassword

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| Username | `string` |  |  |
| Password | `string` |  |  |

## HCloudAutoScalableNodeGroup

<p>Details about (autoscalable) node-groups in HCloud.</p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| MachineType | `string` |  | HCloud machine type.<br>You can browse all available HCloud machine types here : https://hetzner.com/cloud.<br> |
| MinSize | `uint` |  | Minimum number of replicas in the nodegroup.<br> |
| Maxsize | `uint` |  | Maximum number of replicas in the nodegroup.<br> |
| Name | `string` |  | Nodegroup name.<br> |
| Labels | `map[string]string` | [] | Labels that you want to be propagated to each node in the nodegroup.<br><br>Each label should meet one of the following criterias to propagate to each of the nodes :<br><br>  1. Has node-role.kubernetes.io as prefix.<br>  2. Belongs to node-restriction.kubernetes.io domain.<br>  3. Belongs to node.cluster.x-k8s.io domain.<br><br>REFER : https://cluster-api.sigs.k8s.io/developer/architecture/controllers/metadata-propagation#machine.<br> |
| Taints | `[]k8s.io/api/core/v1.Taint` | [] | Taints that you want to be propagated to each node in the nodegroup.<br> |

## HCloudControlPlane

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| MachineType | `string` |  |  |
| Replicas | `uint` |  |  |
| LoadBalancer | `HCloudControlPlaneLoadBalancer` |  |  |

## HCloudControlPlaneLoadBalancer

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| Enabled | `bool` |  |  |
| Region | `string` |  |  |

## HetznerBareMetalConfig

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| WipeDisks | `bool` | false |  |
| InstallImage | `InstallImageConfig` |  |  |
| SSHKeyPair | `HetznerBareMetalSSHKeyPair` |  |  |
| DiskLayoutSetupCommands | `string` |  |  |
| CEPH | `CEPHConfig` |  |  |

## HetznerBareMetalControlPlane

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| Endpoint | `HetznerBareMetalControlPlaneEndpoint` |  |  |
| BareMetalHosts | `[]HetznerBareMetalHost` |  |  |
| DiskLayoutSetupCommands | `string` |  |  |

## HetznerBareMetalControlPlaneEndpoint

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| IsFailoverIP | `bool` |  |  |
| Host | `string` |  |  |

## HetznerBareMetalHost

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| ServerID | `string` |  |  |
| WWNs | `[]string` |  |  |

## HetznerBareMetalNodeGroup

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| BareMetalHosts | `[]HetznerBareMetalHost` |  |  |
| DiskLayoutSetupCommands | `string` |  |  |
| Name | `string` |  | Nodegroup name.<br> |
| Labels | `map[string]string` | [] | Labels that you want to be propagated to each node in the nodegroup.<br><br>Each label should meet one of the following criterias to propagate to each of the nodes :<br><br>  1. Has node-role.kubernetes.io as prefix.<br>  2. Belongs to node-restriction.kubernetes.io domain.<br>  3. Belongs to node.cluster.x-k8s.io domain.<br><br>REFER : https://cluster-api.sigs.k8s.io/developer/architecture/controllers/metadata-propagation#machine.<br> |
| Taints | `[]k8s.io/api/core/v1.Taint` | [] | Taints that you want to be propagated to each node in the nodegroup.<br> |

## HetznerBareMetalSSHKeyPair

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| Name | `string` |  |  |
| PrivateKeyFilePath | `string` |  |  |
| PublicKeyFilePath | `string` |  |  |

## HetznerConfig

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| Mode | `string` | hcloud | The Hetzner mode to use :<br><br>  1. hcloud : Both the control-plane and the nodegroups will be in HCloud.<br><br>  2. bare-metal : Both the control-plane and the nodegroups will be in Hetzner Bare Metal.<br><br>  3. hybrid : The control-plane will be in HCloud, and each node-group can be either in<br>              HCloud or Hetzner Bare Metal.<br> |
| VSwitch | `VSwitchConfig` |  |  |
| HCloud | `HetznerHCloudConfig` |  |  |
| BareMetal | `HetznerBareMetalConfig` |  |  |
| ControlPlane | `HetznerControlPlane` |  |  |
| NodeGroups | `HetznerNodeGroups` |  | Details about node-groups in Hetzner.<br> |

## HetznerControlPlane

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| HCloud | `HCloudControlPlane` |  |  |
| BareMetal | `HetznerBareMetalControlPlane` |  |  |
| Regions | `[]string` |  |  |

## HetznerCredentials

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| APIToken | `string` |  |  |
| Robot | `HetznerRobotCredentials` |  |  |

## HetznerHCloudConfig

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| Zone | `string` |  |  |
| ImageName | `string` | ubuntu-24.04 |  |
| SSHKeyPairName | `string` |  |  |

## HetznerNodeGroups

<p>Details about node-groups in Hetzner.</p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| HCloud | `[]HCloudAutoScalableNodeGroup` |  | Details about node-groups in HCloud.<br> |
| BareMetal | `[]HetznerBareMetalNodeGroup` |  | Details about node-groups in Hetzner Bare Metal.<br> |

## HetznerRobotCredentials

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| User | `string` |  |  |
| Password | `string` |  |  |

## HostPathMountConfig

<p>REFER : "sigs.k8s.io/cluster-api/bootstrap/kubeadm/api/v1beta1".HostPathMount</p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| Name | `string` |  |  |
| HostPath | `string` |  |  |
| MountPath | `string` |  |  |
| PathType | `k8s.io/api/core/v1.HostPathType` |  |  |
| ReadOnly | `bool` | true | Whether the mount should be read-only.<br> |

## InstallImageConfig

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| ImagePath | `string` | /root/.oldroot/nfs/images/Ubuntu-2404-noble-amd64-base.tar.gz |  |
| VG0 | `VG0Config` |  |  |

## KubeAidForkConfig

<p>KubeAid repository specific details.</p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| URL | `string` | https://github.com/Obmondo/KubeAid | KubeAid repository (HTTPS) URL.<br> |
| Version | `string` |  | KubeAid tag.<br> |

## KubePrometheusConfig

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| Version | `string` | v0.15.0 |  |
| GrafanaURL | `string` |  |  |

## KubeaidConfigForkConfig

<p>KubeAid Config repository specific details.</p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| URL | `string` |  | KubeAid repository (HTTPS) URL.<br> |
| Directory | `string` |  | Name of the directory inside your KubeAid Config repository's k8s folder, where the KubeAid<br>Config files for this cluster will be contained.<br><br>When not specified, the directory name will default to the cluster name.<br><br>So, suppose your cluster name is 'staging'. Then, the directory name will default to<br>'staging'. Or you can customize it to something like 'staging.qa'.<br> |

## LocalConfig

<p>Local specific.</p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|

## NodeGroup

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| Name | `string` |  | Nodegroup name.<br> |
| Labels | `map[string]string` | [] | Labels that you want to be propagated to each node in the nodegroup.<br><br>Each label should meet one of the following criterias to propagate to each of the nodes :<br><br>  1. Has node-role.kubernetes.io as prefix.<br>  2. Belongs to node-restriction.kubernetes.io domain.<br>  3. Belongs to node.cluster.x-k8s.io domain.<br><br>REFER : https://cluster-api.sigs.k8s.io/developer/architecture/controllers/metadata-propagation#machine.<br> |
| Taints | `[]k8s.io/api/core/v1.Taint` | [] | Taints that you want to be propagated to each node in the nodegroup.<br> |

## ObmondoConfig

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| CustomerID | `string` |  |  |
| Monitoring | `bool` |  |  |

## SSHKeyPairConfig

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| PublicKeyFilePath | `string` |  |  |
| PrivateKeyFilePath | `string` |  |  |

## SSHPrivateKeyConfig

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| PrivateKeyFilePath | `string` |  |  |

## SecretsConfig

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| Git | `GitCredentials` |  |  |
| AWS | `AWSCredentials` |  |  |
| Azure | `AzureCredentials` |  |  |
| Hetzner | `HetznerCredentials` |  |  |

## UserConfig

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| Name | `string` |  |  |
| SSHPublicKey | `string` |  |  |

## VG0Config

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| Size | `string` | 25G |  |
| RootVolumeSize | `string` | 10G |  |

## VSwitchConfig

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| VLANID | `int` |  |  |
| Name | `string` |  |  |

## WorkloadIdentity

<p></p>

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| OpenIDProviderSSHKeyPair | `SSHKeyPairConfig` |  |  |
