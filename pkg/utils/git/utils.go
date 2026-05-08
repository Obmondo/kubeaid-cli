// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package git

import (
	"context"
	"log/slog"
	"path"
	"strings"

	goGit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/giturl"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/progress"
)

const (
	remoteHEADRefName = "refs/remotes/" + goGit.DefaultRemoteName + "/HEAD"
	remoteBranchPrefix = "refs/remotes/" + goGit.DefaultRemoteName + "/"
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

// GetDefaultBranchName returns the default branch of the 'origin' remote.
//
// Reads refs/remotes/origin/HEAD locally first (no network) — set by
// SetRemoteHEADRef after clone, or lazily cached by this function after
// a network fallback. Falls back to remote.ListContext only when the
// symbolic ref is missing — a legacy clone made before the eager-write
// landed, or one produced by a tool that didn't set the ref. The
// fallback caches its result so subsequent calls hit the local path.
//
// go-git's PlainCloneContext doesn't write refs/remotes/origin/HEAD
// (still missing as of v5.x — verified on disk under
// /tmp/kubeaid-core/...kubeaid-config/.git/refs/remotes/origin/), so
// the local lookup only succeeds for clones we've cached one way or
// another. Each successful network fallback writes the cache, so a
// pre-eager-write legacy clone pays one list-refs touch on first call
// and zero thereafter.
func GetDefaultBranchName(ctx context.Context,
	authMethod transport.AuthMethod,
	repo *goGit.Repository,
) string {
	if ref, err := repo.Reference(plumbing.ReferenceName(remoteHEADRefName), false); err == nil {
		target := ref.Target().String()
		if strings.HasPrefix(target, remoteBranchPrefix) {
			return strings.TrimPrefix(target, remoteBranchPrefix)
		}
	}

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
		if ref.Name() != plumbing.HEAD {
			continue
		}
		target := ref.Target().String()
		defaultBranchName := target[11:] // strip "refs/heads/"

		slog.InfoContext(ctx,
			"Detected default branch name",
			slog.String("branch", defaultBranchName),
		)

		SetRemoteHEADRef(ctx, repo, defaultBranchName)
		return defaultBranchName
	}

	panic("Failed detecting default branch name")
}

// SetRemoteHEADRef writes refs/remotes/origin/HEAD as a symbolic ref
// pointing to refs/remotes/origin/<branch>. Lets GetDefaultBranchName
// answer locally on subsequent calls, skipping the remote list-refs
// round-trip (and the YubiKey touch that goes with it). Best-effort —
// any write failure logs a warning; GetDefaultBranchName's fallback
// path is the safety net.
func SetRemoteHEADRef(ctx context.Context, repo *goGit.Repository, branch string) {
	symbolicRef := plumbing.NewSymbolicReference(
		plumbing.ReferenceName(remoteHEADRefName),
		plumbing.ReferenceName(remoteBranchPrefix+branch),
	)
	if err := repo.Storer.SetReference(symbolicRef); err != nil {
		slog.WarnContext(ctx, "Couldn't cache refs/remotes/origin/HEAD locally",
			slog.Any("err", err))
	}
}
