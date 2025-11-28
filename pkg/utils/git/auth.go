// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package git

import (
	"context"
	"log/slog"

	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
)

func GetGitAuthMethod(ctx context.Context) (authMethod transport.AuthMethod) {
	slog.InfoContext(ctx, "Determining git auth method")

	var err error

	switch {
	// SSH private key and password.
	case config.ParsedGeneralConfig.Git.SSHPrivateKeyConfig != nil:
		authMethod, err = ssh.NewPublicKeysFromFile(
			"git",
			config.ParsedGeneralConfig.Git.PrivateKey,
			config.ParsedSecretsConfig.Git.Password,
		)
		assert.AssertErrNil(ctx, err,
			"Failed generating SSH public key from SSH private key and password for git",
		)
		slog.InfoContext(ctx, "Using SSH private key and password")

	// Username and password.
	case len(config.ParsedSecretsConfig.Git.Password) > 0:
		authMethod = &http.BasicAuth{
			Username: config.ParsedSecretsConfig.Git.Username,
			Password: config.ParsedSecretsConfig.Git.Password,
		}
		slog.InfoContext(ctx, "Using username and password")

	// SSH agent.
	default:
		authMethod, err = ssh.NewSSHAgentAuth("git")
		assert.AssertErrNil(ctx, err, "SSH agent failed")
		slog.InfoContext(ctx, "Using SSH agent")
	}
	return authMethod
}
