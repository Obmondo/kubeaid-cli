// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

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
	generalConfig := config.ParsedGeneralConfig

	// Deploy keys used by ArgoCD to access the KubeAid and KubeAid Config repositories.
	hydrateSSHPrivateKeyConfig(&generalConfig.Cluster.ArgoCD.DeployKeys.Kubeaid)
	hydrateSSHPrivateKeyConfig(&generalConfig.Cluster.ArgoCD.DeployKeys.KubeaidConfig)

	// When using SSH private key to authenticate against git.
	if generalConfig.Git.SSHPrivateKeyConfig != nil {
		hydrateSSHPrivateKeyConfig(generalConfig.Git.SSHPrivateKeyConfig)
	}

	switch globals.CloudProviderName {
	case constants.CloudProviderAzure:
		hydrateSSHKeyPairConfig(
			&generalConfig.Cloud.Azure.WorkloadIdentity.OpenIDProviderSSHKeyPair,
		)

	case constants.CloudProviderHetzner:
		mode := generalConfig.Cloud.Hetzner.Mode

		// When using Hetzner bare-metal.
		if (mode == constants.HetznerModeBareMetal) || (mode == constants.HetznerModeHybrid) {
			hydrateSSHKeyPairConfig(
				&generalConfig.Cloud.Hetzner.BareMetal.SSHKeyPair.SSHKeyPairConfig,
			)
		}

	case constants.CloudProviderBareMetal:
		if generalConfig.Cloud.BareMetal.SSH.PrivateKey != nil {
			hydrateSSHPrivateKeyConfig(generalConfig.Cloud.BareMetal.SSH.PrivateKey)
		}

		// Handle host level SSH config overrides, if any.

		for _, host := range generalConfig.Cloud.BareMetal.ControlPlane.Hosts {
			if (host.SSH != nil) && (host.SSH.PrivateKey != nil) {
				hydrateSSHPrivateKeyConfig(host.SSH.PrivateKey)
			}
		}

		for _, nodeGroup := range generalConfig.Cloud.BareMetal.NodeGroups {
			for _, host := range nodeGroup.Hosts {
				if (host.SSH != nil) && (host.SSH.PrivateKey != nil) {
					hydrateSSHPrivateKeyConfig(host.SSH.PrivateKey)
				}
			}
		}
	}
}

// Reads and validates an SSH key-pair from the provided file paths.
// The key-pair is then stored in the SSH key config struct itself.
func hydrateSSHKeyPairConfig(sshKeyConfig *config.SSHKeyPairConfig) {
	ctx := context.Background()

	// Read and validate the SSH private key.
	hydrateSSHPrivateKeyConfig(&sshKeyConfig.SSHPrivateKeyConfig)

	{
		// Read the SSH public key.
		publicKey, err := os.ReadFile(sshKeyConfig.PublicKeyFilePath)
		assert.AssertErrNil(ctx, err,
			"Failed reading file",
			slog.String("path", sshKeyConfig.PublicKeyFilePath),
		)
		sshKeyConfig.PublicKey = string(publicKey)

		// Validate the SSH public key.
		switch {
		// OpenSSH.
		case strings.HasPrefix(sshKeyConfig.PublicKey, constants.SSHPublicKeyPrefixOpenSSH):

			_, _, _, _, err = ssh.ParseAuthorizedKey(publicKey)
			assert.AssertErrNil(ctx, err,
				"SSH public key is invalid : failed parsing",
				slog.String("path", sshKeyConfig.PublicKeyFilePath),
			)

		//nolint:godox
		// TODO : PEM.
		case strings.HasPrefix(sshKeyConfig.PublicKey, constants.SSHPublicKeyPrefixPEM):
			break

		default:
			slog.ErrorContext(ctx, "Failed identifying SSH public key type")
			os.Exit(1)
		}
	}
}

// Reads and validates an SSH private key from the provided file path.
// The private key is then stored in the SSH private key config struct itself.
func hydrateSSHPrivateKeyConfig(sshPrivateKeyConfig *config.SSHPrivateKeyConfig) {
	ctx := context.Background()

	// Read the SSH private key.

	privateKey, err := os.ReadFile(sshPrivateKeyConfig.PrivateKeyFilePath)
	assert.AssertErrNil(ctx, err,
		"Failed reading file",
		slog.String("path", sshPrivateKeyConfig.PrivateKeyFilePath),
	)
	sshPrivateKeyConfig.PrivateKey = string(privateKey)

	// Validate the SSH private key, based on its type.

	switch {
	// OpenSSH.
	case strings.HasPrefix(sshPrivateKeyConfig.PrivateKey, constants.SSHPrivateKeyPrefixOpenSSH):

		_, err = ssh.ParsePrivateKey(privateKey)
		assert.AssertErrNil(ctx, err,
			"SSH private key is invalid : failed parsing",
			slog.String("path", sshPrivateKeyConfig.PrivateKeyFilePath),
		)

	//nolint:godox
	// TODO : PEM.
	case strings.HasPrefix(sshPrivateKeyConfig.PrivateKey, constants.SSHPrivateKeyPrefixPEM):
		break

	default:
		slog.ErrorContext(ctx, "Failed identifying SSH privaye key type")
		os.Exit(1)
	}
}
