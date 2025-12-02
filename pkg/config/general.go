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
	GeneralConfig struct {
		Git            GitConfig            `yaml:"git"`
		Cluster        ClusterConfig        `yaml:"cluster"        validate:"required"`
		Forks          ForksConfig          `yaml:"forkURLs"       validate:"required"`
		Cloud          CloudConfig          `yaml:"cloud"          validate:"required"`
		KubePrometheus KubePrometheusConfig `yaml:"kubePrometheus"`
		Obmondo        *ObmondoConfig       `yaml:"obmondo"`
	}

	GitConfig struct {
		CABundlePath string `yaml:"caBundlePath"`
		CABundle     []byte `yaml:"caBundle"`

		*SSHPrivateKeyConfig `yaml:",inline"`

		UseSSHAgentAuth bool `yaml:"useSSHAgentAuth"`
	}

	ForksConfig struct {
		KubeaidFork       KubeAidForkConfig       `yaml:"kubeaid"       validate:"required"`
		KubeaidConfigFork KubeaidConfigForkConfig `yaml:"kubeaidConfig" validate:"required"`
	}

	KubeAidForkConfig struct {
		URL     string `yaml:"url"     default:"https://github.com/Obmondo/KubeAid" validate:"notblank"`
		Version string `yaml:"version"                                              validate:"notblank"`
	}

	KubeaidConfigForkConfig struct {
		URL       string `yaml:"url"       validate:"notblank"`
		Directory string `yaml:"directory"`
	}

	ClusterConfig struct {
		Name       string `yaml:"name"       validate:"notblank"`
		K8sVersion string `yaml:"k8sVersion" validate:"notblank"`

		EnableAuditLogging bool `yaml:"enableAuditLogging" default:"True"`

		APIServer APIServerConfig `yaml:"apiServer"`

		AdditionalUsers []UserConfig `yaml:"additionalUsers"`
	}

	/*
		REFER : https://github.com/kubernetes-sigs/cluster-api/blob/main/controlplane/kubeadm/config/crd/bases/controlplane.cluster.x-k8s.io_kubeadmcontrolplanes.yaml.

		NOTE : Generally, refer to the KubeadmControlPlane CRD instead of the corresponding GoLang
		       source types linked below.
		       There are some configuration options which appear in the corresponding GoLang source
		       type, but not in the CRD. If you set those fields, then they get removed by the Kubeadm
		       control-plane provider. This causes the capi-cluster ArgoCD App to always be in an
		       OutOfSync state, resulting to the KubeAid Bootstrap Script not making any progress!
	*/
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

		/*
			Whether the mount should be read-only or not.
			Defaults to true.

			NOTE : If you want the mount to be read-only, then set this true.
			       Otherwise, omit setting this field. It gets removed by the Kubeadm control-plane
			       provider component, which results to the capi-cluster ArgoCD App always being in
			       OutOfSync state.
		*/
		ReadOnly bool `yaml:"readOnly,omitempty"`
	}

	// REFER : "sigs.k8s.io/cluster-api/bootstrap/kubeadm/api/v1beta1".File
	FileConfig struct {
		Path    string `yaml:"path"    validate:"notblank"`
		Content string `yaml:"content" validate:"notblank"`
	}

	UserConfig struct {
		Name         string `yaml:"name"         validate:"required"`
		SSHPublicKey string `yaml:"sshPublicKey" validate:"required"`
	}

	NodeGroup struct {
		Name string `yaml:"name" validate:"notblank"`

		Labels map[string]string `yaml:"labels" default:"[]"`
		Taints []*coreV1.Taint   `yaml:"taints" default:"[]"`
	}

	AutoScalableNodeGroup struct {
		NodeGroup `yaml:",inline"`

		CPU    uint32 `validate:"required"`
		Memory uint32 `validate:"required"`

		MinSize uint `yaml:"minSize" validate:"required"`
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
		Version    string `yaml:"version"              default:"v0.15.0"`
		GrafanaURL string `yaml:"grafanaURL,omitempty"`
	}

	ObmondoConfig struct {
		// nolint: godox
		// TODO: regex validation
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
		Mode string `yaml:"mode" default:"hcloud" validate:"notblank,oneof=bare-metal hcloud hybrid"`

		VSwitch VSwitchConfig `yaml:"vswitch" validate:"required"`

		HCloud    *HetznerHCloudConfig    `yaml:"hcloud"`
		BareMetal *HetznerBareMetalConfig `yaml:"bareMetal"`

		ControlPlane HetznerControlPlane `yaml:"controlPlane" validate:"required"`
		NodeGroups   HetznerNodeGroups   `yaml:"nodeGroups"`
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
		ImagePath               string                     `yaml:"imagePath"               default:"/root/.oldroot/nfs/images/Ubuntu-2404-noble-amd64-base.tar.gz" validate:"notblank"`
		SSHKeyPair              HetznerBareMetalSSHKeyPair `yaml:"sshKeyPair"                                                                                      validate:"required"`
		VG0                     VG0Config                  `yaml:"vg0"`
		DiskLayoutSetupCommands string                     `yaml:"diskLayoutSetupCommands"`
		CEPH                    *CEPHConfig                `yaml:"ceph"`
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

	HetznerNodeGroups struct {
		HCloud    []HCloudAutoScalableNodeGroup `yaml:"hcloud"`
		BareMetal []HetznerBareMetalNodeGroup   `yaml:"bareMetal"`
	}

	HCloudAutoScalableNodeGroup struct {
		AutoScalableNodeGroup `yaml:",inline"`

		MachineType    string `yaml:"machineType" validate:"notblank"`
		RootVolumeSize uint32 `                   validate:"required"`
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
