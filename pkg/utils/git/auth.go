// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package git

import (
	"context"
	"log/slog"

	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
)

func GetGitAuthMethod(ctx context.Context) transport.AuthMethod {
	slog.InfoContext(ctx, "Determining Git auth method")

	var (
		authMethod transport.AuthMethod
		err        error

		gitConfig = config.ParsedGeneralConfig.Git
	)

	switch {
	// SSH private key.
	case gitConfig.SSHPrivateKeyConfig != nil:
		authMethod, err = ssh.NewPublicKeysFromFile(gitConfig.SSHUsername, gitConfig.PrivateKeyFilePath, "")
		assert.AssertErrNil(ctx, err,
			"Failed generating SSH public key from SSH private key and empty passphrase",
		)
		slog.InfoContext(ctx, "Using SSH private key")

	// SSH agent.
	default:
		// TODO : Ensure that SSH_AUTH_SOCK environment variable is defined,
		//        and the corresponding socket file exists.

		authMethod, err = ssh.NewSSHAgentAuth(gitConfig.SSHUsername)
		assert.AssertErrNil(ctx, err, "SSH agent failed")
		slog.InfoContext(ctx, "Using SSH agent")
	}
	return authMethod
}
