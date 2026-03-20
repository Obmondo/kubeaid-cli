// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package git

import (
	"context"
	"log/slog"
	"os"
	"path"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/transport"
	gossh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"golang.org/x/crypto/ssh"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
)

func GetGitAuthMethod(
	ctx context.Context,
	bundledKnownHosts []string,
) transport.AuthMethod {
	slog.InfoContext(ctx, "Determining Git auth method")

	var (
		authMethod transport.AuthMethod
		err        error

		gitConfig = config.ParsedGeneralConfig.Git
	)

	switch {
	// SSH private key.
	case gitConfig.SSHKeyPairConfig != nil:
		authMethod, err = gossh.NewPublicKeysFromFile(
			gitConfig.SSHUsername,
			gitConfig.PrivateKeyFilePath,
			"",
		)
		assert.AssertErrNil(ctx, err,
			"Failed generating SSH public key from SSH private key",
		)
		slog.InfoContext(ctx, "Using SSH private key")

	// SSH agent.
	default:
		authMethod, err = gossh.NewSSHAgentAuth(gitConfig.SSHUsername)
		assert.AssertErrNil(ctx, err, "SSH agent failed")
		slog.InfoContext(ctx, "Using SSH agent")
	}

	// Set host key callback using bundled + user-provided known hosts.
	callback := BuildKnownHostsCallback(ctx, bundledKnownHosts)
	switch a := authMethod.(type) {
	case *gossh.PublicKeys:
		a.HostKeyCallback = callback
	case *gossh.PublicKeysCallback:
		a.HostKeyCallback = callback
	}

	return authMethod
}

// BuildKnownHostsCallback writes bundled known hosts and user-provided
// entries to a file, then returns a host key callback that uses it.
func BuildKnownHostsCallback(
	ctx context.Context,
	bundledKnownHosts []string,
) ssh.HostKeyCallback {
	knownHostsPath := path.Join(
		constants.TempDirectory, "known_hosts",
	)

	f, err := os.Create(knownHostsPath)
	assert.AssertErrNil(ctx, err,
		"Failed creating known hosts file",
	)

	// Write all known host entries (bundled + user-provided).
	allEntries := append(
		bundledKnownHosts,
		config.ParsedGeneralConfig.Git.KnownHosts...,
	)
	_, err = f.WriteString(strings.Join(allEntries, "\n") + "\n")
	assert.AssertErrNil(ctx, err, "Failed writing known hosts")

	f.Close()

	callback, err := gossh.NewKnownHostsCallback(knownHostsPath)
	assert.AssertErrNil(ctx, err,
		"Failed creating known hosts callback",
	)

	return callback
}
