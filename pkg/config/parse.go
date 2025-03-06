package config

import (
	"context"
	"log/slog"
	"os"
	"path"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud/hetzner"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
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

	// Set globals.CloudProviderName and globals.CloudProvider by detecting the underlying
	// cloud-platform being used.
	detectCloudProvider()

	// Set defaults.
	{
		err = defaults.Set(ParsedConfig)
		assert.AssertErrNil(ctx, err, "Failed setting defaults for parsed config")

		// Read cloud credentials from CLI flags and store them in config.
		readCloudCredentialsFromFlagsToConfig(ctx)

		// Read SSH key-pairs from provided file paths and store them in config.
		hydrateSSHKeyConfigs()

		// Hydrate with Audit Logging options (if required).
		hydrateWithAuditLoggingOptions()

		/*
			For each node-group, the CPU and memory of the corresponding VM type need to specified.
			This is required by Cluster AutoScaler, for 2 things to work :

			(1) scale from zero

			(2) when a node in a node-group is cordoned and there is workload-pressure, the node-group
					gets scaled up.
		*/
		// NOTE : Always make sure this gets called after readCloudCredentialsFromFlagsToConfig(),
		//        since the cloud credentials from the parsed config are required to construct the
		//        cloud client.
		hydrateVMSpecs(ctx)
	}

	// Validate.
	validateConfig(ParsedConfig)
}

// Based on the parsed config, detects the underlying cloud-provider name.
// It then sets the value of globals.CloudProviderName and globals.CloudProvider.
func detectCloudProvider() {
	switch {
	case ParsedConfig.Cloud.AWS != nil:
		globals.CloudProviderName = constants.CloudProviderAWS
		globals.CloudProvider = NewAWSCloudProvider()

	case ParsedConfig.Cloud.Azure != nil:
		globals.CloudProviderName = constants.CloudProviderAzure
		panic("unimplemented")

	case ParsedConfig.Cloud.Hetzner != nil:
		globals.CloudProviderName = constants.CloudProviderHetzner
		globals.CloudProvider = hetzner.NewHetznerCloudProvider()

	case ParsedConfig.Cloud.Local != nil:
		globals.CloudProviderName = constants.CloudProviderLocal

	default:
		slog.Error("No cloud specific details provided")
		os.Exit(1)
	}
}

/*
Reads cloud credentials into the config.
The cloud credentials are searched one by one in the following sources :

	(1) CLI Flags / corresponding Environment Variables.

	(2) Config File.

	(3) ~/.aws/config (only in case of AWS :)).

We'll fail during validation if the cloud credentials are found in none of the above sources.
*/
func readCloudCredentialsFromFlagsToConfig(ctx context.Context) {
	switch globals.CloudProviderName {
	case constants.CloudProviderAWS:
		// Try to capture the AWS credentials from CLI flags (or corresponding environment variables).
		if len(AWSAccessKeyID) != 0 {
			slog.InfoContext(ctx,
				"Using AWS credentials captured from the CLI flags (or corresponding environment variables)",
			)

			ParsedConfig.Cloud.AWS.Credentials = AWSCredentials{
				AWSAccessKeyID:     AWSAccessKeyID,
				AWSSecretAccessKey: AWSSecretAccessKey,
				AWSSessionToken:    AWSSessionToken,
			}

			return
		}

		// So the AWS credentials and region were not provided via CLI flags (or corresponding
		// environment variables).
		// We'll now look for them in the config file.
		if len(ParsedConfig.Cloud.AWS.Credentials.AWSAccessKeyID) != 0 {
			slog.InfoContext(ctx, "Using AWS credentials provided via the config file")
			return
		}

		// The AWS credentials and region were not provided via the config file as well.
		// We'll retrieve them from ~/.aws/credentials.
		awsCredentials := mustGetCredentialsFromAWSConfigFile(ctx)
		slog.InfoContext(ctx, "Using AWS credentials from ~/.aws/config")
		ParsedConfig.Cloud.AWS.Credentials = AWSCredentials{
			AWSAccessKeyID:     awsCredentials.AccessKeyID,
			AWSSecretAccessKey: awsCredentials.SecretAccessKey,
			AWSSessionToken:    awsCredentials.SessionToken,
		}

	case constants.CloudProviderAzure:
		panic("unimplemented")

	case constants.CloudProviderHetzner:
		// Try to capture the Hetzner credentials from CLI flags (or corresponding environment
		// variables).
		if len(HetznerAPIToken) != 0 {
			ParsedConfig.Cloud.Hetzner.Credentials = HetznerCredentials{
				HetznerAPIToken,
				HetznerRobotUsername,
				HetznerRobotPassword,
			}

			slog.InfoContext(ctx,
				"Using Hetzner credentials captured from the CLI flags (or corresponding environment variables)",
			)

			return
		}

		// The Hetzner credentials must be then provided in the config file.
		// Otherwise config validation will fail.

	case constants.CloudProviderLocal:
		return

	default:
		panic("unreachable")
	}
}

// Retrieve AWS credentials from the ~/.aws/config file.
// Panics, if any error occurs.
func mustGetCredentialsFromAWSConfigFile(ctx context.Context) *aws.Credentials {
	// Verify that the ~/.aws/config exists and can be opened.
	{
		homeDir, err := os.UserHomeDir()
		assert.AssertErrNil(ctx, err, "Failed getting user's home dir")

		_, err = os.Open(path.Join(homeDir, ".aws/config"))
		assert.AssertErrNil(ctx, err, "Failed opening ~/.aws/config")
	}

	// Now that we have confirmed that the ~/.aws/config file exists, we'll try to read the AWS
	// credentials off of it.

	slog.InfoContext(ctx, "Detected ~/.aws/credentials")

	awsConfig, err := config.LoadDefaultConfig(ctx)
	assert.AssertErrNil(ctx, err, "Failed constructing AWS config from ~/.aws/config")

	awsCredentials, err := awsConfig.Credentials.Retrieve(ctx)
	assert.AssertErrNil(ctx, err, "Failed retrieving AWS credentials from constructed AWS config")

	return &awsCredentials
}

func hydrateSSHKeyConfigs() {
	switch globals.CloudProviderName {
	case constants.CloudProviderHetzner:
		// When using Hetzner Bare Metal.
		if (ParsedConfig.Cloud.Hetzner.HetznerBareMetal != nil) && ParsedConfig.Cloud.Hetzner.HetznerBareMetal.Enabled {
			hydrateSSHKeyConfig(&ParsedConfig.Cloud.Hetzner.HetznerBareMetal.RobotSSHKeyPair)
		}
	}
}

// Reads and validates an SSH key-pair from the provided file paths.
// The key-pair is then stored in the SSH key config struct itself.
func hydrateSSHKeyConfig(sshKeyConfig *SSHKeyPairConfig) {
	ctx := context.Background()

	// Read and validate the SSH public key.

	publicKey, err := os.ReadFile(sshKeyConfig.PublicKeyFilePath)
	assert.AssertErrNil(ctx, err,
		"Failed reading file",
		slog.String("path", sshKeyConfig.PublicKeyFilePath),
	)

	_, _, _, _, err = ssh.ParseAuthorizedKey(publicKey)
	assert.AssertErrNil(ctx, err,
		"SSH public key is invalid : failed parsing",
		slog.String("path", sshKeyConfig.PublicKeyFilePath),
	)

	sshKeyConfig.PublicKey = string(publicKey)

	// Read and validate the SSH private key.

	privateKey, err := os.ReadFile(sshKeyConfig.PrivateKeyFilePath)
	assert.AssertErrNil(ctx, err,
		"Failed reading file",
		slog.String("path", sshKeyConfig.PrivateKeyFilePath),
	)

	_, err = ssh.ParsePrivateKey(privateKey)
	assert.AssertErrNil(ctx, err,
		"SSH private key is invalid : failed parsing",
		slog.String("path", sshKeyConfig.PrivateKeyFilePath),
	)

	sshKeyConfig.PrivateKey = string(privateKey)
}

// For each node-group, fills up the cpu and memory (fetched using the corresponding cloud SDK) of
// the corresponding VM type being used.
func hydrateVMSpecs(ctx context.Context) {
	switch globals.CloudProviderName {
	case constants.CloudProviderAWS:
		for i, nodeGroup := range ParsedConfig.Cloud.AWS.NodeGroups {
			instanceSpecs := globals.CloudProvider.GetVMSpecs(ctx, nodeGroup.InstanceType)

			ParsedConfig.Cloud.AWS.NodeGroups[i].CPU = instanceSpecs.CPU
			ParsedConfig.Cloud.AWS.NodeGroups[i].Memory = instanceSpecs.Memory
		}

	case constants.CloudProviderAzure:
		panic("unimplemented")

	case constants.CloudProviderHetzner:
		panic("unimplemented")

	case constants.CloudProviderLocal:
		return

	default:
		panic("unreachable")
	}
}
