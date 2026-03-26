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

// Returns the Git authentication method which KubeAid CLI will use to interact with the
// KubeAid Config and / or KubeAid repositories.
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
	switch {
	// SSH private key.
	case gitConfig.SSHKeyPairConfig != nil:
		publicKeysAuthMethod, err := gossh.NewPublicKeysFromFile(
			gitConfig.SSHUsername,
			gitConfig.PrivateKeyFilePath,
			"",
		)
		assert.AssertErrNil(ctx, err, "Failed generating SSH public key from SSH private key")

		publicKeysAuthMethod.HostKeyCallback = knownHostsCallback

		authMethod = publicKeysAuthMethod

		slog.InfoContext(ctx, "Using SSH private key")

	// SSH agent.
	default:
		sshAgentAuthMethod, err := gossh.NewSSHAgentAuth(gitConfig.SSHUsername)
		assert.AssertErrNil(ctx, err, "SSH agent failed")

		sshAgentAuthMethod.HostKeyCallback = knownHostsCallback

		authMethod = sshAgentAuthMethod

		slog.InfoContext(ctx, "Using SSH agent")
	}

	return authMethod
}
