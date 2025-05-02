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
	case len(config.ParsedSecretsConfig.Git.SSHPrivateKey) > 0:
		authMethod, err = ssh.NewPublicKeysFromFile(
			"git",
			config.ParsedSecretsConfig.Git.SSHPrivateKey,
			config.ParsedSecretsConfig.Git.Password,
		)
		assert.AssertErrNil(ctx, err,
			"Failed generating SSH public key from SSH private key and password for git",
		)
		slog.Info("Using SSH private key and password")

	// Username and password.
	case len(config.ParsedSecretsConfig.Git.Password) > 0:
		authMethod = &http.BasicAuth{
			Username: config.ParsedSecretsConfig.Git.Username,
			Password: config.ParsedSecretsConfig.Git.Password,
		}
		slog.Info("Using username and password")

	// SSH agent.
	default:
		authMethod, err = ssh.NewSSHAgentAuth("git")
		assert.AssertErrNil(ctx, err, "SSH agent failed")
		slog.Info("Using SSH agent")
	}
	return
}
