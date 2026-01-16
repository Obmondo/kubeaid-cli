// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package config

import (
	coreV1 "k8s.io/api/core/v1"
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
		KubePrometheus KubePrometheusConfig `yaml:"kubePrometheus"`

		// Obmondo customer specific details.
		Obmondo *ObmondoConfig `yaml:"obmondo"`
	}

	// Git specific details, used by KubeAid CLI,
	// to clone repositories from and push changes to the Git server.
	// We enforce the user to use SSH, for authenticating to the Git server.
	GitConfig struct {
		CABundlePath string `yaml:"caBundlePath"`
		CABundle     []byte

		// SSH username.
		SSHUsername string `yaml:"sshUsername" validate:"notblank" default:"git"`

		// Either make KubeAid CLI use the given SSH private key.
		*SSHPrivateKeyConfig `yaml:",inline"`

		// Or, make KubeAid CLI use the SSH Agent.
		// So, you (the one who runs KubeAid CLI) can use your YubiKey.
		UseSSHAgent bool `yaml:"useSSHAgent"`
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
		URL string `yaml:"url" validate:"required"`

		// KubeAid tag.
		Version string `yaml:"version" validate:"notblank"`
	}

	// KubeAid Config repository specific details.
	KubeaidConfigForkConfig struct {
		// KubeAid Config repository SSH URL.
		URL string `yaml:"url" validate:"required"`

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

		// Configuration options for the Kubernetes API server.
		APIServer APIServerConfig `yaml:"apiServer"`

		// Other than the root user, addtional users that you would like to be created in each node.
		// NOTE : Currently, we can't register additional SSH key-pairs against the root user.
		AdditionalUsers []UserConfig `yaml:"additionalUsers"`
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
		VeleroBackupsBucketName        string `yaml:"veleroBackupsBucketName"        validate:"notblank"`
		SealedSecretsBackupsBucketName string `yaml:"sealedSecretsBackupsBucketName" validate:"notblank"`
	}

	SSHKeyPairConfig struct {
		SSHPrivateKeyConfig `yaml:",inline"`

		PublicKeyFilePath string `yaml:"publicKeyFilePath" validate:"notblank"`
		PublicKey         string `                         validate:"notblank"`
	}

	SSHPrivateKeyConfig struct {
		PrivateKeyFilePath string `yaml:"privateKeyFilePath" validate:"notblank"`
		PrivateKey         string `                          validate:"notblank"`
	}

	KubePrometheusConfig struct {
		Version    string `yaml:"version"    default:"v0.15.0"`
		GrafanaURL string `yaml:"grafanaURL"`
	}

	ObmondoConfig struct {
		CustomerID string `yaml:"customerID" validate:"notblank"`
		Monitoring bool   `yaml:"monitoring"`
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
		OpenIDProviderSSHKeyPair SSHKeyPairConfig `yaml:"openIDProviderSSHKeyPair" validate:"notblank"`
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
		// The Hetzner mode to use :
		//
		//   1. hcloud : Both the control-plane and the nodegroups will be in HCloud.
		//
		//   2. bare-metal : Both the control-plane and the nodegroups will be in Hetzner Bare Metal.
		//
		//   3. hybrid : The control-plane will be in HCloud, and each node-group can be either in
		//               HCloud or Hetzner Bare Metal.
		Mode string `yaml:"mode" default:"hcloud" validate:"notblank,oneof=bare-metal hcloud hybrid"`

		VSwitch *VSwitchConfig `yaml:"vswitch"`

		HCloud    *HetznerHCloudConfig    `yaml:"hcloud"`
		BareMetal *HetznerBareMetalConfig `yaml:"bareMetal"`

		ControlPlane HetznerControlPlane `yaml:"controlPlane" validate:"required"`

		// Details about node-groups in Hetzner.
		NodeGroups HetznerNodeGroups `yaml:"nodeGroups"`
	}

	VSwitchConfig struct {
		VLANID int    `yaml:"vlanID"`
		Name   string `yaml:"name"   validate:"notblank"`
	}

	HetznerHCloudConfig struct {
		Zone           string `yaml:"zone"           validate:"notblank"`
		ImageName      string `yaml:"imageName"      validate:"notblank" default:"ubuntu-24.04"`
		SSHKeyPairName string `yaml:"sshKeyPairName" validate:"notblank"`
	}

	HetznerBareMetalConfig struct {
		WipeDisks               bool                       `yaml:"wipeDisks"               default:"false"`
		InstallImage            InstallImageConfig         `yaml:"installImage"`
		SSHKeyPair              HetznerBareMetalSSHKeyPair `yaml:"sshKeyPair"                              validate:"required"`
		DiskLayoutSetupCommands string                     `yaml:"diskLayoutSetupCommands"`
		CEPH                    *CEPHConfig                `yaml:"ceph"`
	}

	InstallImageConfig struct {
		ImagePath string    `yaml:"imagePath" default:"/root/.oldroot/nfs/images/Ubuntu-2404-noble-amd64-base.tar.gz" validate:"notblank"`
		VG0       VG0Config `yaml:"vg0"`
	}

	HetznerBareMetalSSHKeyPair struct {
		Name             string `yaml:"name"    validate:"notblank"`
		SSHKeyPairConfig `       yaml:",inline"`
	}

	VG0Config struct {
		Size           string `yaml:"size"           validate:"notblank" default:"25G"`
		RootVolumeSize string `yaml:"rootVolumeSize" validate:"notblank" default:"10G"`
	}

	CEPHConfig struct {
		DeviceFilter string `yaml:"deviceFilter" validate:"notblank"`
	}

	HetznerControlPlane struct {
		HCloud    *HCloudControlPlane           `yaml:"hcloud"`
		BareMetal *HetznerBareMetalControlPlane `yaml:"bareMetal"`

		Regions []string `yaml:"regions" validate:"required"`
	}

	HCloudControlPlane struct {
		MachineType  string                         `yaml:"machineType"  validate:"notblank"`
		Replicas     uint                           `yaml:"replicas"     validate:"notblank"`
		LoadBalancer HCloudControlPlaneLoadBalancer `yaml:"loadBalancer" validate:"required"`
	}

	HetznerBareMetalControlPlane struct {
		Endpoint                HetznerBareMetalControlPlaneEndpoint `yaml:"endpoint"                validate:"required"`
		BareMetalHosts          []HetznerBareMetalHost               `yaml:"bareMetalHosts"          validate:"required,gt=0"`
		DiskLayoutSetupCommands string                               `yaml:"diskLayoutSetupCommands"`
	}

	HetznerBareMetalControlPlaneEndpoint struct {
		IsFailoverIP bool   `yaml:"isFailoverIP"`
		Host         string `yaml:"host"         validate:"ip"`
	}

	HCloudControlPlaneLoadBalancer struct {
		Enabled bool   `yaml:"enabled" validate:"required"`
		Region  string `yaml:"region"  validate:"notblank"`
	}

	// Details about node-groups in Hetzner.
	HetznerNodeGroups struct {
		// Details about node-groups in HCloud.
		HCloud []HCloudAutoScalableNodeGroup `yaml:"hcloud"`

		// Details about node-groups in Hetzner Bare Metal.
		BareMetal []HetznerBareMetalNodeGroup `yaml:"bareMetal"`
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

		BareMetalHosts          []HetznerBareMetalHost `yaml:"bareMetalHosts"          validate:"required,gt=0"`
		DiskLayoutSetupCommands string                 `yaml:"diskLayoutSetupCommands"`
	}

	HetznerBareMetalHost struct {
		ServerID string   `yaml:"serverID" validate:"notblank"`
		WWNs     []string `yaml:"wwns"     validate:"required,gt=0"`
	}
)

// Bare Metal specific.
type (
	BareMetalConfig struct {
		SSH BareMetalSSHConfig `yaml:"ssh"`

		ControlPlane BareMetalControlPlane `yaml:"controlPlane" validate:"required"`
		NodeGroups   []BareMetalNodeGroup  `yaml:"nodeGroups"`
	}

	BareMetalSSHConfig struct {
		Port       uint                 `yaml:"port"       validate:"required" default:"22"`
		PrivateKey *SSHPrivateKeyConfig `yaml:"privateKey"`
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
