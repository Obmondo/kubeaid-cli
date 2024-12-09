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

		Region         string             `yaml:"region"`
		BastionEnabled bool               `yaml:"bastionEnabled" default:"True"`
		ControlPlane   ControlPlaneConfig `yaml:"controlPlane" validate:"required"`
		NodeGroups     []NodeGroups       `yaml:"nodeGroups" validate:"required"`
		SSHKeyName     string             `yaml:"sshKeyName" validate:"required,notblank"`

		DisasterRecovery *AWSDisasterRecoveryConfig `yaml:"disasterRecovery"`
	}

	AWSCredentials struct {
		AWSAccessKey    string `validate:"required,notblank"`
		AWSSecretKey    string `validate:"required,notblank"`
		AWSSessionToken string
		AWSRegion       string `validate:"required,notblank"`
	}

	ControlPlaneConfig struct {
		Replicas     int       `yaml:"replicas" validate:"required"`
		InstanceType string    `yaml:"instanceType" validate:"required,notblank"`
		AMI          AMIConfig `yaml:"ami" validate:"required"`
	}

	NodeGroups struct {
		Name           string            `yaml:"name" validate:"required,notblank"`
		Replicas       int               `yaml:"replicas" validate:"required"`
		InstanceType   string            `yaml:"instanceType" validate:"required,notblank"`
		SSHKeyName     string            `yaml:"sshKeyName" validate:"required,notblank"`
		AMI            AMIConfig         `yaml:"ami" validate:"required"`
		RootVolumeSize int               `yaml:"rootVolumeSize" validate:"required"`
		Labels         map[string]string `yaml:"labels" default:"[]"`
		Taints         []*coreV1.Taint   `yaml:"taints" default:"[]"`
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
		Robot           bool             `yaml:"robot" validate:"required"`
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
