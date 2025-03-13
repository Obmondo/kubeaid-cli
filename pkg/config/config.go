package config

import (
	"context"

	coreV1 "k8s.io/api/core/v1"
)

type (
	Config struct {
		CustomerID string           `yaml:"customerID"`
		Git        GitConfig        `yaml:"git" validate:"required"`
		Cluster    ClusterConfig    `yaml:"cluster" validate:"required"`
		Forks      ForksConfig      `yaml:"forks" validate:"required"`
		Cloud      CloudConfig      `yaml:"cloud" validate:"required"`
		Monitoring MonitoringConfig `yaml:"monitoring"`
	}

	GitConfig struct {
		Username        string `yaml:"username"`
		Password        string `yaml:"password"`
		SSHPrivateKey   string `yaml:"sshPrivateKey"`
		UseSSHAgentAuth bool   `yaml:"useSSHAgentAuth"`
	}

	ForksConfig struct {
		KubeaidForkURL       string `yaml:"kubeaid" default:"https://github.com/Obmondo/KubeAid"`
		KubeaidConfigForkURL string `yaml:"kubeaidConfig" validate:"required,notblank"`
	}

	ClusterConfig struct {
		Name           string `yaml:"name" validate:"required,notblank"`
		K8sVersion     string `yaml:"k8sVersion" validate:"required,notblank"`
		KubeaidVersion string `yaml:"kubeaidVersion" validate:"required,notblank"`

		EnableAuditLogging bool `yaml:"enableAuditLogging" default:"True"`

		APIServer APIServerConfig `yaml:"apiServer"`

		AdditionalUsers []UserConfig `yaml:"additionalUsers"`
	}

	// REFER : https://github.com/kubernetes-sigs/cluster-api/blob/main/controlplane/kubeadm/config/crd/bases/controlplane.cluster.x-k8s.io_kubeadmcontrolplanes.yaml.
	//
	// NOTE : Generally, refer to the KubeadmControlPlane CRD instead of the corresponding GoLang
	//        source types linked below.
	//        There are some configuration options which appear in the corresponding GoLang source type,
	//        but not in the CRD. If you set those fields, then they get removed by the Kubeadm
	//        control-plane provider. This causes the capi-cluster ArgoCD App to always be in an
	//        OutOfSync state, resulting to the KubeAid Bootstrap Script not making any progress!
	APIServerConfig struct {
		ExtraArgs    map[string]string     `yaml:"extraArgs" default:"{}"`
		ExtraVolumes []HostPathMountConfig `yaml:"extraVolumes" default:"[]"`
		Files        []FileConfig          `yaml:"files" default:"[]"`
	}

	// REFER : "sigs.k8s.io/cluster-api/bootstrap/kubeadm/api/v1beta1".HostPathMount
	HostPathMountConfig struct {
		Name      string              `yaml:"name" validate:"required,notblank"`
		HostPath  string              `yaml:"hostPath" validate:"required,notblank"`
		MountPath string              `yaml:"mountPath" validate:"required,notblank"`
		PathType  coreV1.HostPathType `yaml:"pathType" validate:"required"`

		// Whether the mount should be read-only or not.
		// Defaults to true.
		//
		// NOTE : If you want the mount to be read-only, then set this true.
		//        Otherwise, omit setting this field. It gets removed by the Kubeadm control-plane
		//        provider component, which results to the capi-cluster ArgoCD App always being in
		//        OutOfSync state.
		ReadOnly bool `yaml:"readOnly,omitempty"`
	}

	// REFER : "sigs.k8s.io/cluster-api/bootstrap/kubeadm/api/v1beta1".File
	FileConfig struct {
		Path    string `yaml:"path" validate:"required,notblank"`
		Content string `yaml:"content" validate:"required,notblank"`
	}

	UserConfig struct {
		Name         string `yaml:"name" validate:"required"`
		SSHPublicKey string `yaml:"sshPublicKey" validate:"required"`
	}

	NodeGroup struct {
		Name string `yaml:"name" validate:"required,notblank"`

		MinSize uint `yaml:"minSize" validate:"required"`
		Maxsize uint `yaml:"maxSize" validate:"required"`

		CPU    uint32 `yaml:"cpu"`
		Memory uint32 `yaml:"memory"`

		Labels map[string]string `yaml:"labels" default:"[]"`
		Taints []*coreV1.Taint   `yaml:"taints" default:"[]"`
	}

	CloudConfig struct {
		AWS     *AWSConfig     `yaml:"aws"`
		Hetzner *HetznerConfig `yaml:"hetzner"`
		Azure   *AzureConfig   `yaml:"azure"`
		Local   *LocalConfig   `yaml:"local"`
	}

	SSHKeyPairConfig struct {
		PublicKeyFilePath string `yaml:"publicKeyFilePath" validate:"required,notblank"`
		PublicKey         string `validate:"required,notblank"`

		PrivateKeyFilePath string `yaml:"privateKeyFilePath" validate:"required,notblank"`
		PrivateKey         string `validate:"required,notblank"`
	}

	MonitoringConfig struct {
		KubePrometheusVersion string `yaml:"kubePrometheusVersion" default:"v0.14.0"`
		GrafanaURL            string `yaml:"grafanaURL"`
		ConnectObmondo        bool   `yaml:"connectObmondo" default:"False"`
	}
)

// AWS specific.
type (
	AWSConfig struct {
		Credentials AWSCredentials `yaml:"credentials"`
		Region      string         `yaml:"region" validate:"required,notblank"`

		BastionEnabled bool            `yaml:"bastionEnabled" default:"True"`
		VPCID          *string         `yaml:"vpcID"`
		ControlPlane   AWSControlPlane `yaml:"controlPlane" validate:"required"`
		NodeGroups     []AWSNodeGroup  `yaml:"nodeGroups" validate:"required"`
		SSHKeyName     string          `yaml:"sshKeyName" validate:"required,notblank"`

		DisasterRecovery *AWSDisasterRecovery `yaml:"disasterRecovery"`
	}

	AWSCredentials struct {
		AWSAccessKeyID     string `yaml:"accessKeyID" validate:"required,notblank"`
		AWSSecretAccessKey string `yaml:"secretAccessKey" validate:"required,notblank"`
		AWSSessionToken    string `yaml:"sessionToken"`
	}

	AWSControlPlane struct {
		LoadBalancerScheme string `yaml:"loadBalancerScheme" default:"internet-facing" validate:"required,notblank"`
		Replicas           uint32 `yaml:"replicas" validate:"required"`
		InstanceType       string `yaml:"instanceType" validate:"required,notblank"`
		AMI                AMI    `yaml:"ami" validate:"required"`
	}

	AWSNodeGroup struct {
		NodeGroup      `yaml:",inline"`
		InstanceType   string `yaml:"instanceType" validate:"required,notblank"`
		SSHKeyName     string `yaml:"sshKeyName" validate:"required,notblank"`
		AMI            AMI    `yaml:"ami" validate:"required"`
		RootVolumeSize uint   `yaml:"rootVolumeSize" validate:"required"`
	}

	AMI struct {
		ID string `yaml:"id" validate:"required,notblank"`
	}

	AWSDisasterRecovery struct {
		VeleroBackupsS3BucketName       string `yaml:"veleroBackupsS3BucketName" validate:"required,notblank"`
		SealedSecretsBackupS3BucketName string `yaml:"sealedSecretsBackupS3BucketName" validate:"required,notblank"`
	}
)

// Hetzner specific.
type (
	HetznerConfig struct {
		Credentials HetznerCredentials

		HCloud           HCloud            `yaml:"hcloud" validate:"required"`
		HetznerBareMetal *HetznerBareMetal `yaml:"robot"`
	}

	HetznerCredentials struct {
		HetznerAPIToken      string `validate:"required,notblank"`
		HetznerRobotUsername string `validate:"required,notblank"`
		HetznerRobotPassword string `validate:"required,notblank"`
	}

	HCloud struct {
		SSHKeyName   string             `yaml:"sshKeyName" validate:"required,notblank"`
		Enabled      bool               `yaml:"enabled"`
		ControlPlane HCloudControlPlane `yaml:"controlPlane"`
		NodeGroups   []HCloudNodeGroup  `yaml:"nodeGroups"`
	}

	HCloudControlPlane struct {
		LoadBalancer HetznerControlPlaneLoadBalancer `yaml:"loadBalancer"`
		Regions      []string                        `yaml:"regions"`
		MachineType  string                          `yaml:"machineType" validate:"required,notblank"`
		Replicas     int                             `yaml:"replicas" validate:"required"`
	}

	HetznerControlPlaneLoadBalancer struct {
		Enabled bool   `yaml:"enabled" validate:"required"`
		Region  string `yaml:"region" validate:"required,notblank"`
	}

	HCloudNodeGroup struct {
		NodeGroup     `yaml:",inline"`
		MachineType   string                  `yaml:"machineType" validate:"required,notblank"`
		FailureDomain string                  `yaml:"failureDomain" validate:"required,notblank"`
		SSHKeys       []HCloudNodeGroupSSHKey `yaml:"sshKeys" validate:"required"`
	}

	HCloudNodeGroupSSHKey struct {
		Name string `yaml:"name" validate:"required,notblank"`
	}

	HetznerBareMetal struct {
		Enabled         bool                         `yaml:"enabled" validate:"required"`
		RobotSSHKeyPair SSHKeyPairConfig             `yaml:"robotSSHKey" validate:"required"`
		ControlPlane    HetznerBareMetalControlPlane `yaml:"controlPlane"`
		NodeGroups      []HetznerBareMetalNodeGroup  `yaml:"nodeGroups"`
	}

	HetznerBareMetalControlPlane struct {
		Endpoint HetznerControlPlaneEndpoint `yaml:"endpoint" validate:"required,notblank"`
		Nodes    []HetznerBareMetalNode      `yaml:"nodes"`
	}

	HetznerControlPlaneEndpoint struct {
		Host string `yaml:"host" validate:"required,notblank"`
		Port int    `yaml:"port"`
	}

	HetznerBareMetalNodeGroup struct {
		NodeGroup `yaml:",inline"`
		Nodes     []HetznerBareMetalNode `yaml:"nodes" validate:"required"`
	}

	HetznerBareMetalNode struct {
		Name string `yaml:"name" validate:"required,notblank"`

		// WWN (World Wide Name) is the unique identifier.
		WWN []string `yaml:"wwn" validate:"required,notblank"`
	}
)

// Azure specific.
type (
	AzureConfig struct {
		TenantID                     string `yaml:"tenantID" validate:"required,notblank"`
		SubscriptionID               string `yaml:"subscriptionID" validate:"required,notblank"`
		ClientIDUserAssignedIdentity string `yaml:"clientIDUserAssignedIdentity" validate:"required,notblank"`

		Location string `yaml:"location" validate:"required,notblank"`

		StorageAccountName                   string `yaml:"storageAccountName" validate:"required,notblank"`
		WorkloadIdentitySSHPublicKeyFilePath string `yaml:"workloadIdentitySSHPublicKeyFilePath" validate:"required,notblank"`

		SSHPublicKey string            `yaml:"sshPublicKey" validate:"required,notblank"`
		ClientSecret AzureClientSecret `yaml:"clientSecret" validate:"required"`

		ControlPlane AzureSelfManagedControlPlane `yaml:"controlPlane" validate:"required"`
	}

	AzureSelfManagedControlPlane struct {
		DiskSizeGB       uint32 `yaml:"diskSizeGB" validate:"required"`
		SKU              string `yaml:"sku" validate:"required,notblank"`
		Replicas         uint32 `yaml:"replicas" validate:"required"`
		LoadBalancerType string `yaml:"loadBalancerType" validate:"required,notblank" default:"Public"`
	}

	AzureClientSecret struct {
		ControlPlane string `yaml:"controlPlane" validate:"required,notblank"`
		WorkerNode   string `yaml:"workerNode" validate:"required,notblank"`
	}

	AKS struct {
		Enabled bool `yaml:"enabled" validate:"required"`
	}
)

// Local specific.
type (
	LocalConfig struct{}
)

var ParsedConfig = &Config{}

// Read config file from the given file path. Then, parse and validate it.
func InitConfig() {
	parseConfigFile(context.Background(), ConfigFilePath)
}
