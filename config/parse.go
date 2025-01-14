package config

import (
	"context"
	"log/slog"
	"os"

	"github.com/Obmondo/kubeaid-bootstrap-script/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/utils/logger"
	"github.com/creasty/defaults"
	"golang.org/x/crypto/ssh"
	"gopkg.in/yaml.v3"
)

func parseConfigFile(ctx context.Context, configFilePath string) {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("path", configFilePath),
	})

	configFileContents, err := os.ReadFile(configFilePath)
	assert.AssertErrNil(ctx, err, "Failed reading config file")

	ParseConfig(ctx, string(configFileContents))
}

func ParseConfig(ctx context.Context, configAsString string) {
	err := yaml.Unmarshal([]byte(configAsString), ParsedConfig)
	assert.AssertErrNil(ctx, err, "Failed unmarshalling config")

	slog.InfoContext(ctx, "Parsed config")

	// Set defaults.
	{
		err = defaults.Set(ParsedConfig)
		assert.AssertErrNil(ctx, err, "Failed setting defaults for parsed config")

		// Read cloud credentials from CLI flags and store them in config.
		readCloudCredentialsFromFlagsToConfig()

		// Read SSH key-pairs from provided file paths and store them in config.
		hydrateSSHKeyConfigs()

		// Hydrate with Audit Logging options (if required).
		hydrateWithAuditLoggingOptions()
	}

	// Validate.
	validateConfig(ParsedConfig)
}

func readCloudCredentialsFromFlagsToConfig() {
	switch {
	case ParsedConfig.Cloud.AWS != nil:
		ParsedConfig.Cloud.AWS.Credentials = AWSCredentials{
			AWSAccessKey,
			AWSSecretKey,
			AWSSessionToken,
			AWSRegion,
		}

	case ParsedConfig.Cloud.Hetzner != nil:
		ParsedConfig.Cloud.Hetzner.Credentials = HetznerCredentials{
			HetznerAPIToken,
			HetznerRobotUser,
			HetznerRobotPassword,
		}
	}
}

func hydrateSSHKeyConfigs() {
	switch {
	case ParsedConfig.Cloud.Hetzner != nil:
		hydrateSSHKeyConfig(&ParsedConfig.Cloud.Hetzner.RobotSSHKeyPair)
	}
}

// Reads and validates an SSH key-pair from the provided file paths.
// The key-pair is then stored in the SSH key config struct itself.
func hydrateSSHKeyConfig(sshKeyConfig *SSHKeyPairConfig) {
	ctx := context.Background()

	// Read and validate the SSH public key.

	publicKey, err := os.ReadFile(sshKeyConfig.PublicKeyFilePath)
	assert.AssertErrNil(ctx, err, "Failed reading file", slog.String("path", sshKeyConfig.PublicKeyFilePath))

	_, _, _, _, err = ssh.ParseAuthorizedKey(publicKey)
	assert.AssertErrNil(ctx, err, "SSH public key is invalid : failed parsing", slog.String("path", sshKeyConfig.PublicKeyFilePath))

	sshKeyConfig.PublicKey = string(publicKey)

	// Read and validate the SSH private key.

	privateKey, err := os.ReadFile(sshKeyConfig.PrivateKeyFilePath)
	assert.AssertErrNil(ctx, err, "Failed reading file", slog.String("path", sshKeyConfig.PrivateKeyFilePath))

	_, err = ssh.ParsePrivateKey(privateKey)
	assert.AssertErrNil(ctx, err, "SSH private key is invalid : failed parsing", slog.String("path", sshKeyConfig.PrivateKeyFilePath))

	sshKeyConfig.PrivateKey = string(privateKey)
}
