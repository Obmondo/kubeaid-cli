// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package git

import (
	"context"
	"log/slog"
	"path"

	goGit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/giturl"
)

// GetRepoDir returns the local on-disk path where the given repository
// will be cloned: <TempDir>/<host>/<owner>/<repo>.
func GetRepoDir(parsedURL *giturl.ParsedURL) string {
	return path.Join(constants.TempDirectory,
		parsedURL.Host, parsedURL.Owner, parsedURL.Repo,
	)
}

// ParseURL is a thin wrapper over giturl.Parse, kept here so callers
// in the git package can use the shorter `git.ParseURL` name and to
// preserve the previous package layout.
func ParseURL(url string) (*giturl.ParsedURL, error) {
	return giturl.Parse(url)
}

func GetDefaultBranchName(ctx context.Context,
	authMethod transport.AuthMethod,
	repo *goGit.Repository,
) string {
	remote, err := repo.Remote(goGit.DefaultRemoteName)
	assert.AssertErrNil(ctx, err, "Failed getting repo 'origin' remote")

	refs, err := retryGitOperationWithResult(
		ctx,
		"list refs for origin remote",
		func() ([]*plumbing.Reference, error) {
			return remote.ListContext(ctx, &goGit.ListOptions{
				Auth:     authMethod,
				CABundle: config.ParsedGeneralConfig.Git.CABundle,
			})
		},
	)
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
