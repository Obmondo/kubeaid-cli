// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package parser

import (
	"context"
	"crypto"
	"encoding/pem"
	"log/slog"
	"net"
	"os"
	"strings"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
)

func hydrateSSHKeyPairConfigs() {
	generalConfig := config.ParsedGeneralConfig

	// Deploy keys used by ArgoCD to access the KubeAid and KubeAid Config repositories.
	deployKeys := &generalConfig.Cluster.ArgoCD.DeployKeys
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

// Reads and validates an SSH key-pair. Two sourcing paths:
//
//  1. UseSSHAgent=false (default): read PrivateKeyFilePath as an
//     OpenSSH private key, parse it, derive PublicKey + Fingerprint
//     from the parsed key. PrivateKey is the raw bytes (used by the
//     Hetzner NAT-gateway SSH client when no agent is available).
//
//  2. UseSSHAgent=true: dial SSH_AUTH_SOCK and ask the agent for
//     its loaded identities. The private key stays in the agent
//     (yubikey hardware module); PrivateKey field stays empty and
//     downstream SSH clients authenticate via the agent socket
//     instead.
//
// Either path populates PublicKey + Fingerprint so the rest of the
// pipeline (HCloud SSH key upload, sealed-secret rendering, etc.)
// is sourcing-agnostic.
func hydrateSSHKeyPairConfig(sshKeyPairConfig *config.SSHKeyPairConfig) {
	if sshKeyPairConfig.UseSSHAgent {
		hydrateSSHKeyPairFromAgent(sshKeyPairConfig)
		return
	}
	hydrateSSHKeyPairFromFile(sshKeyPairConfig)
}

func hydrateSSHKeyPairFromFile(sshKeyPairConfig *config.SSHKeyPairConfig) {
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

	sshKeyPairConfig.PublicKey = string(ssh.MarshalAuthorizedKey(parsedPublicKey))

	sshKeyPairConfig.Fingerprint = ssh.FingerprintLegacyMD5(parsedPublicKey)
}

func hydrateSSHKeyPairFromAgent(sshKeyPairConfig *config.SSHKeyPairConfig) {
	ctx := context.Background()

	socketPath := os.Getenv(constants.EnvNameSSHAuthSock)
	assert.Assert(ctx, socketPath != "",
		"useSSHAgent=true but SSH_AUTH_SOCK is unset — start ssh-agent or plug in your yubikey")

	conn, err := net.Dial("unix", socketPath) //nolint:gosec // G704: dialing the operator's own SSH agent socket from $SSH_AUTH_SOCK.
	assert.AssertErrNil(ctx, err, "Failed dialling SSH agent socket")
	defer func() { _ = conn.Close() }()

	identities, err := agent.NewClient(conn).List()
	assert.AssertErrNil(ctx, err, "Failed listing SSH agent identities")
	assert.Assert(ctx, len(identities) > 0,
		"SSH agent has no keys loaded (yubikey unplugged?), but useSSHAgent=true was set in config")

	// Use the first identity. Operators with multiple keys loaded
	// can hint via load order; kubeaid-cli doesn't guess between
	// them. *agent.Key satisfies ssh.PublicKey, so we hand it
	// straight to the OpenSSH marshallers.
	key := identities[0]

	sshKeyPairConfig.PublicKey = string(ssh.MarshalAuthorizedKey(key))
	sshKeyPairConfig.Fingerprint = ssh.FingerprintLegacyMD5(key)
	// PrivateKey stays empty — downstream consumers (Hetzner NAT
	// gateway SSH client) detect this and route through the agent
	// socket via os.Getenv(SSH_AUTH_SOCK).
}
