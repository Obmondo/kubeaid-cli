// Copyright 2025 Obmondo
// SPDX-License-Identifier: Apache-2.0

package parser

import (
	"context"
	"crypto"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"

	"github.com/charmbracelet/huh"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/term"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/globals"
	"github.com/Obmondo/kubeaid-cli/pkg/utils"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/logger"
)

func hydrateSSHKeyPairConfigs() {
	generalConfig := config.ParsedGeneralConfig

	// Deploy keys used by ArgoCD to access the KubeAid and KubeAid Config repositories.
	deployKeys := &generalConfig.Cluster.ArgoCD.DeployKeys
	if deployKeys.Kubeaid != nil {
		hydrateSSHKeyPairConfig(deployKeys.Kubeaid)
		assertDeployKeyIsMaterialised(deployKeys.Kubeaid, "kubeaid")
	}
	hydrateSSHKeyPairConfig(&deployKeys.KubeaidConfig)
	assertDeployKeyIsMaterialised(&deployKeys.KubeaidConfig, "kubeaidConfig")

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

		publicKeyMatches, err := publicKeyFileMatchesFingerprint(
			openIDProviderSSHKeyPair.PublicKeyFilePath,
			openIDProviderSSHKeyPair.Fingerprint,
		)
		assert.AssertErrNil(ctx, err, "Failed validating provided SSH public key file")
		assert.Assert(ctx,
			publicKeyMatches,
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

// assertDeployKeyIsMaterialised fails the run when a deploy key hydrated to
// no private key material.
//
// ArgoCD's deploy keys are the one pair that cannot be agent-held: they are
// rendered into a SealedSecret and live in the cluster, so the material has
// to exist here. hydrateSSHKeyPairFromAgent leaves PrivateKey empty by
// design, and the sealed-secret template would embed that emptiness happily —
// producing a cluster that bootstraps, reports success, and then silently
// never syncs because ArgoCD cannot authenticate to the repository.
//
// Cheap check, whole class of failure, and it fires at parse time rather than
// an hour into a bootstrap.
func assertDeployKeyIsMaterialised(sshKeyPairConfig *config.SSHKeyPairConfig, name string) {
	ctx := logger.AppendSlogAttributesToCtx(context.Background(), []slog.Attr{
		slog.String("deploy-key", name),
	})

	assert.Assert(ctx, sshKeyPairConfig.PrivateKey != "",
		fmt.Sprintf(
			"ArgoCD deploy key %q resolved to no private key material — it is sealed into a Secret and read inside the cluster, so it cannot come from an SSH agent; point privateKeyFilePath at a key file, or let an install token deliver one",
			name,
		),
	)
}

// Test seams: the prompt and the TTY check both need a real terminal, so unit
// tests override these to drive the branching without one. Same shape as
// pkg/core/netbird's.
var (
	stdinIsTerminal         = func() bool { return term.IsTerminal(int(os.Stdin.Fd())) }
	promptSSHPrivateKeyPath = runSSHPrivateKeyPathForm
)

// defaultSSHPrivateKeyPath is what the prompt starts on: the name almost every
// key has, and the one the portal's wizard offers.
const defaultSSHPrivateKeyPath = "~/.ssh/id_ed25519"

// askForSSHPrivateKeyPath asks where the private key is, for a key pair
// general.yaml named no path for.
//
// Config that arrives without a path is not a broken config — it is an
// unfinished one, and the answer is on the operator's own machine. Failing
// would tell someone holding the key that they cannot proceed.
//
// Unattended runs cannot answer, so those still fail, with a message naming
// every way out rather than the "no such file" that an empty path used to
// produce.
func askForSSHPrivateKeyPath(ctx context.Context) string {
	assert.Assert(
		ctx,
		stdinIsTerminal(),
		"No SSH private key file path set, useSSHAgent is false, and there is no terminal to ask on: set privateKeyFilePath in general.yaml, enable useSSHAgent, or re-run with an install token that delivers the key",
	)

	path := defaultSSHPrivateKeyPath
	err := promptSSHPrivateKeyPath(&path)
	assert.AssertErrNil(ctx, err, "Failed asking for the SSH private key file path")

	return path
}

// runSSHPrivateKeyPathForm asks for a path and refuses to return one that is
// not a readable SSH private key — better to correct it here than to accept it
// and fail on the next line.
func runSSHPrivateKeyPathForm(path *string) error {
	return huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Path to your SSH private key:").
				Description("general.yaml names no privateKeyFilePath for this key. Where is it on this machine?").
				Value(path).
				Validate(validateSSHPrivateKeyAtPath),
		),
	).Run()
}

// validateSSHPrivateKeyAtPath checks the answer names a key this run can
// actually use. Encrypted keys pass: the passphrase is supplied later, the
// same allowance pkg/config/prompt makes.
func validateSSHPrivateKeyAtPath(path string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("a path is required")
	}

	absolutePath, err := utils.ToAbsolutePath(path)
	if err != nil {
		return fmt.Errorf("resolving %s: %w", path, err)
	}

	privateKey, err := os.ReadFile(absolutePath)
	if err != nil {
		return fmt.Errorf("reading %s: %w", absolutePath, err)
	}

	if _, err := ssh.ParseRawPrivateKey(privateKey); err != nil {
		var missingPassphrase *ssh.PassphraseMissingError
		if errors.As(err, &missingPassphrase) {
			return nil
		}
		return fmt.Errorf("%s is not an SSH private key: %w", absolutePath, err)
	}
	return nil
}

func hydrateSSHKeyPairFromFile(sshKeyPairConfig *config.SSHKeyPairConfig) {
	ctx := logger.AppendSlogAttributesToCtx(context.Background(), []slog.Attr{
		slog.String("private-key-file-path", sshKeyPairConfig.PrivateKeyFilePath),
	})

	// An empty path means nothing filled it in: general.yaml was written
	// without one, or it came from the portal and the install token carried no
	// key for this slot. The operator almost always has the key — they just
	// never said where — so ask rather than end the run over it.
	if sshKeyPairConfig.PrivateKeyFilePath == "" {
		sshKeyPairConfig.PrivateKeyFilePath = askForSSHPrivateKeyPath(ctx)
	}

	// Expand "~" before reading. os.ReadFile does no shell expansion, so
	// ~/.ssh/id_ed25519 — which is what the portal's wizard offers by
	// default — would otherwise be looked for inside a directory literally
	// named "~".
	privateKeyFilePath, err := utils.ToAbsolutePath(sshKeyPairConfig.PrivateKeyFilePath)
	assert.AssertErrNil(ctx, err, "Failed resolving SSH private key file path")
	sshKeyPairConfig.PrivateKeyFilePath = privateKeyFilePath

	// Read the SSH private key.
	privateKey, err := os.ReadFile(privateKeyFilePath)
	assert.AssertErrNil(ctx, err, "Failed reading SSH private key file")

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

// publicKeyFileMatchesFingerprint reports whether the SSH public key at
// authorizedKeyFilePath (OpenSSH authorized_keys format) has the given
// legacy-MD5 fingerprint.
func publicKeyFileMatchesFingerprint(authorizedKeyFilePath, fingerprint string) (bool, error) {
	publicKeyBytes, err := os.ReadFile(authorizedKeyFilePath)
	if err != nil {
		return false, fmt.Errorf("reading SSH public key file: %w", err)
	}

	parsedPublicKey, _, _, _, err := ssh.ParseAuthorizedKey(publicKeyBytes)
	if err != nil {
		return false, fmt.Errorf("parsing SSH public key file: %w", err)
	}

	return ssh.FingerprintLegacyMD5(parsedPublicKey) == fingerprint, nil
}
