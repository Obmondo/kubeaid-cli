// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package git

import (
	"context"
	"log/slog"
	"net/url"

	goGit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
)

func GetDefaultBranchName(ctx context.Context,
	authMethod transport.AuthMethod,
	repo *goGit.Repository,
) string {
	remote, err := repo.Remote(goGit.DefaultRemoteName)
	assert.AssertErrNil(ctx, err, "Failed getting repo 'origin' remote")

	refs, err := remote.List(&goGit.ListOptions{
		Auth:     authMethod,
		CABundle: config.ParsedGeneralConfig.Git.CABundle,
	})
	assert.AssertErrNil(ctx, err, "Failed listing refs for 'origin' remote")

	for _, ref := range refs {
		if ref.Name() == plumbing.HEAD {
			target := ref.Target().String()

			defaultBranchName := target[11:] // Remove "refs/heads/" prefix.
			slog.InfoContext(ctx,
				"Detected default branch name",
				slog.String("branch", defaultBranchName),
			)

			return defaultBranchName
		}
	}

	panic("Failed detecting default branch name")
}

// Returns hostname of customer's git server.
func GetCustomerGitServerHostName(ctx context.Context) string {
	kubeaidConfigURL, err := url.Parse(config.ParsedGeneralConfig.Forks.KubeaidConfigForkURL)
	assert.AssertErrNil(ctx, err, "Failed parsing KubeAid config URL")

	return kubeaidConfigURL.Hostname()
}
