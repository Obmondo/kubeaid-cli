package config

type (
	SecretsConfig struct {
		Git     GitCredentials      `yaml:"git"`
		AWS     *AWSCredentials     `yaml:"aws"`
		Azure   *AzureCredentials   `yaml:"azure"`
		Hetzner *HetznerCredentials `yaml:"hetzner"`
	}

	GitCredentials struct {
		Username      string `yaml:"username"`
		Password      string `yaml:"password"`
		SSHPrivateKey string `yaml:"sshPrivateKey"`
	}

	AWSCredentials struct {
		AWSAccessKeyID     string `yaml:"accessKeyID" validate:"required,notblank"`
		AWSSecretAccessKey string `yaml:"secretAccessKey" validate:"required,notblank"`
		AWSSessionToken    string `yaml:"sessionToken"`
	}

	AzureCredentials struct {
		ClientID     string `yaml:"clientID" validate:"required,notblank"`
		ClientSecret string `yaml:"clientSecret" validate:"required,notblank"`
	}

	HetznerCredentials struct {
		HetznerAPIToken      string `validate:"required,notblank"`
		HetznerRobotUsername string `validate:"required,notblank"`
		HetznerRobotPassword string `validate:"required,notblank"`
	}
)
