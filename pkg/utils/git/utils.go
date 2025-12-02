// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package git

import (
	"context"
	"log/slog"
	"net/url"
	"path"

	goGit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	gogiturl "github.com/kubescape/go-git-url"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
)

// GetRepoDir( ) gets invoked pretty frequently by utils.GetKubeAidConfigDir( ). And everytime we
// don't want to reparse the same repository URL.
// So, let's cache the return values of GetRepoDir( ) in this Hashmap.
var repoDirs = map[string]string{}

// Returns path to the directory where the given repository will be cloned.
func GetRepoDir(url string) string {
	// Check whether the return value is cached.
	// If yes, then return that. We don't want to parse the same repository URL again and again.
	repoDir, ok := repoDirs[url]
	if ok {
		return repoDir
	}

	parsedURL, err := gogiturl.NewGitURL(url)
	assert.AssertErrNil(context.Background(), err, "Failed parsing git URL", slog.String("url", url))

	return path.Join(constants.TempDirectory,
		parsedURL.GetHostName(), parsedURL.GetOwnerName(), parsedURL.GetRepoName(),
	)
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

// Returns hostname of customer's git server.
func GetCustomerGitServerHostName(ctx context.Context) string {
	kubeaidConfigURL, err := url.Parse(config.ParsedGeneralConfig.Forks.KubeaidConfigFork.URL)
	assert.AssertErrNil(ctx, err, "Failed parsing KubeAid config URL")

	return kubeaidConfigURL.Hostname()
}
