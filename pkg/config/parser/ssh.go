package parser

import (
	"context"
	"log/slog"
	"os"
	"strings"

	"golang.org/x/crypto/ssh"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
)

func hydrateSSHKeyConfigs() {
	switch globals.CloudProviderName {
	case constants.CloudProviderAzure:
		hydrateSSHKeyConfig(
			&config.ParsedGeneralConfig.Cloud.Azure.WorkloadIdentity.OpenIDProviderSSHKeyPair,
		)

	case constants.CloudProviderHetzner:
		mode := config.ParsedGeneralConfig.Cloud.Hetzner.Mode

		// When using Hetzner bare-metal.
		if (mode == constants.HetznerModeBareMetal) || (mode == constants.HetznerModeHybrid) {
			hydrateSSHKeyConfig(
				&config.ParsedGeneralConfig.Cloud.Hetzner.RescueHCloudSSHKeyPair.SSHKeyPairConfig,
			)
		}
	}
}

// Reads and validates an SSH key-pair from the provided file paths.
// The key-pair is then stored in the SSH key config struct itself.
func hydrateSSHKeyConfig(sshKeyConfig *config.SSHKeyPairConfig) {
	ctx := context.Background()

	// Read the SSH key-pair.

	publicKey, err := os.ReadFile(sshKeyConfig.PublicKeyFilePath)
	assert.AssertErrNil(ctx, err,
		"Failed reading file",
		slog.String("path", sshKeyConfig.PublicKeyFilePath),
	)
	sshKeyConfig.PublicKey = string(publicKey)

	privateKey, err := os.ReadFile(sshKeyConfig.PrivateKeyFilePath)
	assert.AssertErrNil(ctx, err,
		"Failed reading file",
		slog.String("path", sshKeyConfig.PrivateKeyFilePath),
	)
	sshKeyConfig.PrivateKey = string(privateKey)

	// Validate the SSH key-pair based on its type.

	switch {
	// OpenSSH.
	case strings.HasPrefix(sshKeyConfig.PublicKey, constants.SSHPublicKeyPrefixOpenSSH):

		_, _, _, _, err = ssh.ParseAuthorizedKey(publicKey)
		assert.AssertErrNil(ctx, err,
			"SSH public key is invalid : failed parsing",
			slog.String("path", sshKeyConfig.PublicKeyFilePath),
		)

		_, err = ssh.ParsePrivateKey(privateKey)
		assert.AssertErrNil(ctx, err,
			"SSH private key is invalid : failed parsing",
			slog.String("path", sshKeyConfig.PrivateKeyFilePath),
		)

	//nolint:godox
	// TODO : PEM.
	case strings.HasPrefix(sshKeyConfig.PublicKey, constants.SSHPublicKeyPrefixPEM):
		break

	default:
		slog.ErrorContext(ctx, "Failed identifying SSH key-pair type using SSH public key prefix")
		os.Exit(1)
	}
}
