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
		Name       string `yaml:"name" validate:"required,notblank"`
		K8sVersion string `yaml:"k8sVersion" validate:"required,notblank"`

		EnableAuditLogging bool `yaml:"enableAuditLogging"`

		APIServer APIServerConfig `yaml:"apiServer"`
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

	CloudConfig struct {
		AWS     *AWSConfig     `yaml:"aws"`
		Hetzner *HetznerConfig `yaml:"hetzner"`
		Azure   *AzureConfig   `yaml:"azure"`
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
		Credentials AWSCredentials

		BastionEnabled bool               `yaml:"bastionEnabled" default:"True"`
		VPCID          *string            `yaml:"vpcID"`
		ControlPlane   ControlPlaneConfig `yaml:"controlPlane" validate:"required"`
		NodeGroups     []NodeGroups       `yaml:"nodeGroups" validate:"required"`
		SSHKeyName     string             `yaml:"sshKeyName" validate:"required,notblank"`

		DisasterRecovery *AWSDisasterRecoveryConfig `yaml:"disasterRecovery"`
	}

	AWSCredentials struct {
		AWSAccessKey    string `yaml:"accessKey" validate:"required,notblank"`
		AWSSecretKey    string `yaml:"secretKey" validate:"required,notblank"`
		AWSSessionToken string `yaml:"sessionToken"`
		AWSRegion       string `yaml:"region" validate:"required,notblank"`
	}

	ControlPlaneConfig struct {
		Replicas     uint      `yaml:"replicas" validate:"required"`
		InstanceType string    `yaml:"instanceType" validate:"required,notblank"`
		AMI          AMIConfig `yaml:"ami" validate:"required"`
	}

	NodeGroups struct {
		Name string `yaml:"name" validate:"required,notblank"`

		MinSize uint `yaml:"minSize" validate:"required"`
		Maxsize uint `yaml:"maxSize" validate:"required"`

		InstanceType   string    `yaml:"instanceType" validate:"required,notblank"`
		SSHKeyName     string    `yaml:"sshKeyName" validate:"required,notblank"`
		AMI            AMIConfig `yaml:"ami" validate:"required"`
		RootVolumeSize uint      `yaml:"rootVolumeSize" validate:"required"`

		Labels map[string]string `yaml:"labels" default:"[]"`
		Taints []*coreV1.Taint   `yaml:"taints" default:"[]"`
	}

	AMIConfig struct {
		ID string `yaml:"id" validate:"required,notblank"`
	}

	AWSDisasterRecoveryConfig struct {
		VeleroBackupsS3BucketName       string `yaml:"veleroBackupsS3BucketName" validate:"required,notblank"`
		SealedSecretsBackupS3BucketName string `yaml:"sealedSecretsBackupS3BucketName" validate:"required,notblank"`
	}
)

// Hetzner specific.
type (
	HetznerConfig struct {
		Credentials HetznerCredentials

		// Robot is Hetzner's administration panel for dedicated root servers, colocation, Storage Boxes,
		// and domains (via the Domain Registration Robot add-on).
		RobotSSHKeyPair SSHKeyPairConfig `yaml:"robotSSHKey" validate:"required"`

		ControlPlaneEndpoint string                        `yaml:"controlPlaneEndpoint" validate:"required,notblank"`
		BareMetalNodes       map[string]HetznerNodeConfigs `yaml:"bareMetalNodes" validate:"required"`
	}

	HetznerCredentials struct {
		HetznerAPIToken      string `validate:"required,notblank"`
		HetznerRobotUser     string `validate:"required,notblank"`
		HetznerRobotPassword string `validate:"required,notblank"`
	}

	HetznerNodeConfigs struct {
		Name string   `yaml:"name" validate:"required,notblank"`
		WWN  []string `yaml:"wwn" validate:"required,notblank"` // World Wide Name, a unique identifier.
	}
)

// Azure specific.
type (
	AzureConfig struct{}
)

var ParsedConfig = &Config{}

// Read config file from the given file path. Then, parse and validate it.
func InitConfig() {
	parseConfigFile(context.Background(), ConfigFilePath)
}
