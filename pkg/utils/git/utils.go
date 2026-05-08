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
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/progress"
)

// GetRepoDir returns the local on-disk path where the given repository
// will be cloned: <TempDir>/<host>/<owner>/<repo>. Uses HostName so
// non-default SSH ports (e.g. ":2223") don't leak the colon into the
// path — tools like docker's -v <src>:<dst> volume spec choke on it.
func GetRepoDir(parsedURL *giturl.ParsedURL) string {
	return path.Join(constants.TempDirectory,
		parsedURL.HostName(), parsedURL.Owner, parsedURL.Repo,
	)
}

// originShortName returns "owner/repo" for the given repo's origin
// remote. Used in YubiKey-touch prompts so the operator sees which
// repository they're authorizing the SSH op against. Falls back to
// "repo" when the URL can't be parsed — the prompt is informational,
// best-effort is fine.
func originShortName(repo *goGit.Repository) string {
	remote, err := repo.Remote(goGit.DefaultRemoteName)
	if err != nil || remote == nil || len(remote.Config().URLs) == 0 {
		return "repo"
	}
	parsed, err := giturl.Parse(remote.Config().URLs[0])
	if err != nil {
		return "repo"
	}
	return parsed.Owner + "/" + parsed.Repo
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

	releaseListTouch := progress.FromCtx(ctx).RequestYubiKeyTouch(
		"look up default branch on " + originShortName(repo),
	)
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
	releaseListTouch()
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
