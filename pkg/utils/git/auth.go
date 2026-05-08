// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package git

import (
	"context"
	"log/slog"

	"github.com/go-git/go-git/v5/plumbing/transport"
	gossh "github.com/go-git/go-git/v5/plumbing/transport/ssh"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
)

// gitAuthMode reports which transport-auth construction path
// GetGitAuthMethod should take for the operator's git config.
type gitAuthMode int

const (
	// gitAuthModeAgent: dial $SSH_AUTH_SOCK; the agent owns the
	// private key (YubiKey-backed in our typical case). Default
	// when no private-key-file is configured, or when UseSSHAgent
	// is explicitly set.
	gitAuthModeAgent gitAuthMode = iota

	// gitAuthModePrivateKeyFile: read an unencrypted private key
	// from disk and use it directly. Selected only when the
	// operator gave a non-empty PrivateKeyFilePath AND did not opt
	// into the agent.
	gitAuthModePrivateKeyFile
)

// gitAuthModeFor selects the auth path based on the operator's git
// config. Pure function — split out from GetGitAuthMethod so the
// routing logic can be unit-tested without touching the filesystem,
// the SSH agent, or known-hosts files.
//
// The SSHKeyPairConfig is non-nil whenever ANY of its fields are set
// (including UseSSHAgent=true alone), so a nil-only check would mis-
// route YubiKey/agent operators into the file path with an empty path.
// The full predicate guards against that: pointer non-nil, UseSSHAgent
// false, AND PrivateKeyFilePath non-empty.
func gitAuthModeFor(gitConfig config.GitConfig) gitAuthMode {
	if gitConfig.SSHKeyPairConfig != nil &&
		!gitConfig.UseSSHAgent &&
		gitConfig.PrivateKeyFilePath != "" {
		return gitAuthModePrivateKeyFile
	}
	return gitAuthModeAgent
}

// Returns the Git authentication method which KubeAid CLI will use to interact with the
// KubeAid Config and / or KubeAid repositories.
//
// The operator-supplied private key (file path) is expected to be
// UNENCRYPTED — we pass an empty passphrase to gossh.NewPublicKeysFromFile.
// Encrypted keys (BEGIN OPENSSH ENCRYPTED ...) fail at construction
// time. Operators who need a passphrased key should use the SSH agent
// path instead, which holds the decrypted material in memory.
func GetGitAuthMethod(ctx context.Context) transport.AuthMethod {
	slog.InfoContext(ctx, "Determining Git auth method")

	gitConfig := config.ParsedGeneralConfig.Git

	// Create the known hosts file, which contains
	// known hosts of common Git repo hosting providers (like Azure DevOps, GitLab etc.), and
	// any extra known hosts specified by the user.
	createKnownHostsFile(ctx)

	knownHostsCallback, err := gossh.NewKnownHostsCallback(constants.OutputPathKnownHostsFile)
	assert.AssertErrNil(ctx, err, "Failed creating known hosts callback")

	var authMethod transport.AuthMethod
	switch gitAuthModeFor(gitConfig) {
	case gitAuthModePrivateKeyFile:
		publicKeysAuthMethod, err := gossh.NewPublicKeysFromFile(
			gitConfig.SSHUsername,
			gitConfig.PrivateKeyFilePath,
			"",
		)
		assert.AssertErrNil(ctx, err, "Failed generating SSH public key from SSH private key")

		publicKeysAuthMethod.HostKeyCallback = knownHostsCallback

		authMethod = publicKeysAuthMethod

		slog.InfoContext(ctx, "Using SSH private key")

	default: // gitAuthModeAgent
		sshAgentAuthMethod, err := gossh.NewSSHAgentAuth(gitConfig.SSHUsername)
		assert.AssertErrNil(ctx, err, "SSH agent failed")

		sshAgentAuthMethod.HostKeyCallback = knownHostsCallback

		authMethod = sshAgentAuthMethod

		slog.InfoContext(ctx, "Using SSH agent")
	}

	return authMethod
}
