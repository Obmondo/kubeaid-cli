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
	gogiturl "github.com/kubescape/go-git-url"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
)

// Returns path to the directory where the given repository will be cloned.
func GetRepoDir(parsedURL gogiturl.IGitURL) string {
	return path.Join(constants.TempDirectory,
		parsedURL.GetHostName(), parsedURL.GetOwnerName(), parsedURL.GetRepoName(),
	)
}

func MustParseURL(ctx context.Context, sshURL string) gogiturl.IGitURL {
	parsedURL, err := gogiturl.NewGitURL(sshURL)
	assert.AssertErrNil(ctx, err, "Failed parsing SSH URL of Git repository")

	return parsedURL
}

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
