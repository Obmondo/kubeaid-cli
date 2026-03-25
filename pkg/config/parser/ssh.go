// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package parser

import (
	"context"
	"crypto"
	"encoding/pem"
	"log/slog"
	"os"
	"strings"

	"golang.org/x/crypto/ssh"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
)

func hydrateSSHKeyPairConfigs() {
	generalConfig := config.ParsedGeneralConfig

	// Deploy keys used by ArgoCD to access the KubeAid and KubeAid Config repositories.
	deployKeys := generalConfig.Cluster.ArgoCD.DeployKeys
	if deployKeys.Kubeaid != nil {
		hydrateSSHKeyPairConfig(deployKeys.Kubeaid)
	}
	hydrateSSHKeyPairConfig(&deployKeys.KubeaidConfig)

	// When using SSH private key to authenticate against git.
	if generalConfig.Git.SSHKeyPairConfig != nil {
		hydrateSSHKeyPairConfig(generalConfig.Git.SSHKeyPairConfig)
	}

	switch globals.CloudProviderName {
	case constants.CloudProviderAzure:
		openIDProviderSSHKeyPair := generalConfig.Cloud.Azure.WorkloadIdentity.OpenIDProviderSSHKeyPair

		hydrateSSHKeyPairConfig(&openIDProviderSSHKeyPair.SSHKeyPairConfig)

		// Ensure that the provided SSH public key file contains the correct SSH public key.

		ctx := logger.AppendSlogAttributesToCtx(context.Background(), []slog.Attr{
			slog.String("public-key-file-path", openIDProviderSSHKeyPair.PublicKeyFilePath),
		})

		providedPublicKey, err := os.ReadFile(openIDProviderSSHKeyPair.PublicKeyFilePath)
		assert.AssertErrNil(ctx, err, "Failed reading SSH public key file")

		parsedProvidedPublicKey, err := ssh.ParsePublicKey(providedPublicKey)
		assert.AssertErrNil(ctx, err, "Failed parsing provided SSH public")

		providedPublicKeyFingerprint := ssh.FingerprintSHA256(parsedProvidedPublicKey)
		assert.Assert(ctx,
			(providedPublicKeyFingerprint == openIDProviderSSHKeyPair.Fingerprint),
			"Provided SSH public key isn't derived from the SSH private key",
			slog.String("private-key-file-path", openIDProviderSSHKeyPair.PrivateKeyFilePath),
		)

	case constants.CloudProviderHetzner:
		hydrateSSHKeyPairConfig(&generalConfig.Cloud.Hetzner.SSHKeyPair.SSHKeyPairConfig)

	case constants.CloudProviderBareMetal:
		bareMetalConfig := generalConfig.Cloud.BareMetal

		if bareMetalConfig.SSH.SSHKeyPairConfig != nil {
			hydrateSSHKeyPairConfig(bareMetalConfig.SSH.SSHKeyPairConfig)
		}

		// Handle host level SSH config overrides, if any.

		for _, host := range bareMetalConfig.ControlPlane.Hosts {
			if (host.SSH != nil) && (host.SSH.SSHKeyPairConfig != nil) {
				hydrateSSHKeyPairConfig(host.SSH.SSHKeyPairConfig)
			}
		}

		for _, nodeGroup := range bareMetalConfig.NodeGroups {
			for _, host := range nodeGroup.Hosts {
				if (host.SSH != nil) && (host.SSH.SSHKeyPairConfig != nil) {
					hydrateSSHKeyPairConfig(host.SSH.SSHKeyPairConfig)
				}
			}
		}
	}
}

// Reads and validates an SSH key-pair from the provided file paths.
// The key-pair is then stored in the SSH key config struct itself.
func hydrateSSHKeyPairConfig(sshKeyPairConfig *config.SSHKeyPairConfig) {
	ctx := logger.AppendSlogAttributesToCtx(context.Background(), []slog.Attr{
		slog.String("private-key-file-path", sshKeyPairConfig.PrivateKeyFilePath),
	})

	// Read the SSH private key.
	privateKey, err := os.ReadFile(sshKeyPairConfig.PrivateKeyFilePath)
	assert.AssertErrNil(ctx, err, "Failed reading file")

	sshKeyPairConfig.PrivateKey = strings.TrimSpace(string(privateKey))

	// Ensure that the serialization format is OpenSSH.
	block, _ := pem.Decode(privateKey)
	assert.Assert(ctx,
		((block != nil) && (block.Type == constants.PEMBlockTypeOpenSSHPrivateKey)),
		"Serialization format for SSH private key isn't OpenSSH",
	)

	// Parse the SSH private key.
	parsedPrivateKey, err := ssh.ParseRawPrivateKey(privateKey)
	assert.AssertErrNil(ctx, err, "Failed to parse SSH private key")

	// Get the public key and fingerprint,
	// and store them in the SSHKeyPairConfig struct itself.

	signer, ok := parsedPrivateKey.(crypto.Signer)
	assert.Assert(ctx, ok, "Failed getting crypto signer from SSH private key")

	parsedPublicKey, err := ssh.NewPublicKey(signer.Public())
	assert.AssertErrNil(ctx, err, "Failed getting SSH public key")

	sshKeyPairConfig.PublicKey = string(parsedPublicKey.Marshal())

	sshKeyPairConfig.Fingerprint = ssh.FingerprintSHA256(parsedPublicKey)
}
