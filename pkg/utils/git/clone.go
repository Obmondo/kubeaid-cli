// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package git

import (
	"context"
	"errors"
	"log/slog"
	"os"

	goGit "github.com/go-git/go-git/v5"
	goGitConfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/giturl"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
)

// CloneRepoOptions controls CloneRepo's re-run-path fetch scope. The
// default behavior (no options, or PinnedRef empty) is for repos
// where kubeaid-cli walks history on the default branch — kubeaid-
// config, where isCommitPresentInBranch in WaitUntilPRMerged needs
// the default branch's commit history. Set PinnedRef for repos
// where kubeaid-cli only ever HardResetRepoToRef to a fixed tag/
// branch (kubeaid-fork): the re-run path skips the default-branch
// dance and fetches only the pinned ref, saving network on every
// re-run.
type CloneRepoOptions struct {
	// PinnedRef is the tag or branch name the caller will hard-reset
	// to after CloneRepo returns. When set, the re-run path narrows
	// its fetch to just this ref instead of the default branch +
	// everything else. Commit-hash values are rejected by
	// validateKubeAidForkVersion at config parse time, so this is
	// guaranteed to be a tag or branch name.
	PinnedRef string
}

// Clones the given git repository, if that isn't already done.
// When the repo is already cloned, it checks out to the default branch and fetches the latest
// changes.
//
// Pass CloneRepoOptions to override the re-run fetch behavior — see
// CloneRepoOptions doc. Variadic parameter for backward compat with
// existing callers that don't need the override.
func CloneRepo(ctx context.Context, url string, authMethod transport.AuthMethod, opts ...CloneRepoOptions) *goGit.Repository {
	var cloneOpts CloneRepoOptions
	if len(opts) > 0 {
		cloneOpts = opts[0]
	}

	// Determine the path, where this repository will be / is cloned.
	parsed, err := ParseURL(url)
	assert.AssertErrNil(ctx, err, "Failed parsing Git repository URL", slog.String("url", url))
	path := GetRepoDir(parsed)

	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("repo", url), slog.String("path", path),
	})

	// For HTTPs URLs, no auth is needed (public repos only).
	// If the clone fails, the repo is likely private and requires an SSH URL.
	if giturl.IsHTTP(url) {
		authMethod = nil
	}

	// When the repo is already cloned.
	if _, err := os.ReadDir(path); err == nil {
		repo, err := goGit.PlainOpen(path)
		if err != nil && errors.Is(err, goGit.ErrRepositoryNotExists) {
			assert.AssertErrNil(ctx, err, "Failed opening existing git repo")
		}

		workTree, err := repo.Worktree()
		assert.AssertErrNil(ctx, err, "Failed getting repo worktree")

		if cloneOpts.PinnedRef != "" {
			// Read-only consumer (kubeaid-fork): fetch only the pinned
			// ref. Skip CheckoutToDefaultBranchAndFetchUpdates' default-
			// branch dance — HardResetRepoToRef immediately after will
			// land us on the pinned ref anyway, and the caller doesn't
			// walk history past that ref.
			refreshPinnedRef(ctx, repo, workTree, cloneOpts.PinnedRef, authMethod)
			return repo
		}

		// Default path (kubeaid-config): checkout default branch + fetch
		// just that branch. All changes in the current branch get discarded.
		CheckoutToDefaultBranchAndFetchUpdates(ctx, repo, workTree, authMethod)

		return repo
	}

	// Clone the repo.
	slog.InfoContext(ctx, "Cloning repo")

	defer requestTouchIfAuth(ctx, "clone "+parsed.Owner+"/"+parsed.Repo, authMethod)()

	var repo *goGit.Repository
	if cloneOpts.PinnedRef != "" {
		// Read-only consumer (kubeaid-fork): shallow-clone just the
		// pinned ref. Avoids pulling the entire repo + every tag on
		// first run — which for kubeaid is a large repo, multiple
		// minutes of network on slow links.
		repo, err = clonePinnedRef(ctx, path, url, cloneOpts.PinnedRef, authMethod)
	} else {
		// Default path (kubeaid-config): full clone, all branches,
		// all tags. We push feature branches to this repo and walk
		// default-branch history in WaitUntilPRMerged, so we need
		// the full ref set.
		repo, err = retryGitOperationWithResult(ctx, "clone repository", func() (*goGit.Repository, error) {
			return goGit.PlainCloneContext(ctx, path, false, &goGit.CloneOptions{
				URL:      url,
				Auth:     authMethod,
				CABundle: config.ParsedGeneralConfig.Git.CABundle,
			})
		})
	}

	if giturl.IsHTTP(url) &&
		(errors.Is(err, transport.ErrAuthenticationRequired) || errors.Is(err, transport.ErrAuthorizationFailed)) {
		slog.ErrorContext(ctx,
			"HTTPS clone failed: private repo detected, switch to SSH URL",
			slog.String("url", url),
		)
		os.Exit(1)
	}

	if errors.Is(err, transport.ErrEmptyRemoteRepository) &&
		(url == config.ParsedGeneralConfig.Forks.KubeaidConfigFork.URL) {
		// Remote KubeAid Config repository is empty.
		// So, we need to initialize the repository locally,
		// add the remote repository as 'origin',
		// and create and push an empty commit.
		repo = initRepo(ctx,
			path,
			config.ParsedGeneralConfig.Forks.KubeaidConfigFork.URL,
			authMethod,
		)
	} else {
		assert.AssertErrNil(ctx, err, "Failed cloning repo")
	}

	// Cache the upstream default branch as a local symbolic ref for
	// non-pinned clones (go-git's PlainCloneContext doesn't write
	// refs/remotes/origin/HEAD itself). Lets GetDefaultBranchName
	// answer from disk on subsequent calls — saves a remote list-refs
	// round-trip per call, and the YubiKey touch that goes with it.
	//
	// Skipped for pinned-ref clones: HEAD on a shallow tag clone is
	// detached at the tag's commit (head.Name().Short() == "HEAD"),
	// and even on a single-branch clone "main" might just be the
	// pinned branch and not the upstream default. kubeaid-fork
	// callers never need GetDefaultBranchName anyway.
	if cloneOpts.PinnedRef == "" {
		if head, err := repo.Head(); err == nil {
			SetRemoteHEADRef(ctx, repo, head.Name().Short())
		}
	}

	return repo
}

// clonePinnedRef shallow-clones url at pinnedRef into path and returns
// the resulting repo. Tries as a tag first (production users almost
// always pin to a tagged release); on NoMatchingRefSpecError, cleans
// up the partial clone directory and retries as a branch.
//
// Depth: 1 + Tags: NoTags + SingleBranch keeps the on-disk repo
// minimal — kubeaid-cli only HardResetRepoToRef against pinnedRef and
// never walks the broader history, so there's nothing extra to gain
// from a fuller clone.
func clonePinnedRef(ctx context.Context,
	path, url, pinnedRef string,
	authMethod transport.AuthMethod,
) (*goGit.Repository, error) {
	repo, err := tryShallowClone(ctx, path, url,
		plumbing.NewTagReferenceName(pinnedRef), authMethod)
	if err == nil {
		return repo, nil
	}

	var noMatch goGit.NoMatchingRefSpecError
	if !errors.As(err, &noMatch) {
		return nil, err
	}

	// Tag didn't exist remotely — must be a branch. PlainCloneContext
	// may have created path with partial state before failing; remove
	// it so the branch retry starts from a clean slate.
	if rmErr := os.RemoveAll(path); rmErr != nil {
		return nil, rmErr
	}
	return tryShallowClone(ctx, path, url,
		plumbing.NewBranchReferenceName(pinnedRef), authMethod)
}

func tryShallowClone(ctx context.Context,
	path, url string,
	refName plumbing.ReferenceName,
	authMethod transport.AuthMethod,
) (*goGit.Repository, error) {
	return retryGitOperationWithResult(ctx, "clone repository", func() (*goGit.Repository, error) {
		return goGit.PlainCloneContext(ctx, path, false, &goGit.CloneOptions{
			URL:           url,
			Auth:          authMethod,
			CABundle:      config.ParsedGeneralConfig.Git.CABundle,
			ReferenceName: refName,
			SingleBranch:  true,
			Depth:         1,
			Tags:          goGit.NoTags,
		})
	})
}

// refreshPinnedRef brings an already-cloned repo's pinned ref up to
// date with origin, without touching the default branch. Used by
// CloneRepo's re-run path for repos like kubeaid-fork where
// kubeaid-cli only ever HardResetRepoToRef to a fixed tag/branch and
// never walks the default branch's history. The caller is expected
// to invoke HardResetRepoToRef immediately after.
//
// One YubiKey touch in the common case: the first clone fetched
// everything (tags + remote-tracking branches), so the pinned ref
// already lives locally as either refs/tags/<v> or
// refs/remotes/origin/<v>. We probe local state, pick the matching
// refspec, and fetch only that. Two touches only when the operator
// bumped pinnedRef to something the local repo's never seen — we try
// the most-likely refspec, fall back to the other on
// NoMatchingRefSpecError.
func refreshPinnedRef(ctx context.Context,
	repo *goGit.Repository,
	workTree *goGit.Worktree,
	pinnedRef string,
	authMethod transport.AuthMethod,
) {
	removeUnstagedChanges(ctx, repo, workTree)

	primary, fallback := pinnedRefSpecs(repo, pinnedRef)

	err := fetchPinnedRefSpec(ctx, repo, pinnedRef, primary, authMethod)
	if err == nil {
		return
	}

	var noMatch goGit.NoMatchingRefSpecError
	if !errors.As(err, &noMatch) {
		assert.AssertErrNil(ctx, err, "Failed fetching pinned ref",
			slog.String("ref", pinnedRef))
	}

	// Primary refspec didn't match remotely — pinnedRef must be the
	// other kind of ref. Fall back.
	err = fetchPinnedRefSpec(ctx, repo, pinnedRef, fallback, authMethod)
	assert.AssertErrNil(ctx, err,
		"Failed fetching pinned ref (tried both tag and branch)",
		slog.String("ref", pinnedRef))
}

// pinnedRefSpecs returns the (most-likely, fallback) pair of refspecs
// for fetching pinnedRef. It uses local state from the prior full
// clone — refs/tags/<v> for tags, refs/remotes/origin/<v> for
// branches — to pick the right one first. Falls back to tag-first
// when local state has neither, since production users almost always
// pin to a tagged release.
func pinnedRefSpecs(repo *goGit.Repository, pinnedRef string) (primary, fallback goGitConfig.RefSpec) {
	tagSpec := goGitConfig.RefSpec("+refs/tags/" + pinnedRef + ":refs/tags/" + pinnedRef)
	branchSpec := goGitConfig.RefSpec("+refs/heads/" + pinnedRef + ":refs/heads/" + pinnedRef)

	if _, err := repo.Reference(
		plumbing.ReferenceName("refs/tags/"+pinnedRef), false,
	); err == nil {
		return tagSpec, branchSpec
	}
	if _, err := repo.Reference(
		plumbing.ReferenceName(remoteBranchPrefix+pinnedRef), false,
	); err == nil {
		return branchSpec, tagSpec
	}
	return tagSpec, branchSpec
}

func fetchPinnedRefSpec(ctx context.Context,
	repo *goGit.Repository,
	pinnedRef string,
	refSpec goGitConfig.RefSpec,
	authMethod transport.AuthMethod,
) error {
	releaseFetchTouch := requestTouchIfAuth(ctx,
		"fetch "+pinnedRef+" from "+originShortName(repo), authMethod,
	)
	defer releaseFetchTouch()

	err := retryGitOperation(ctx, "fetch pinned ref", func() error {
		return repo.FetchContext(ctx, &goGit.FetchOptions{
			Auth:     authMethod,
			CABundle: config.ParsedGeneralConfig.Git.CABundle,
			RefSpecs: []goGitConfig.RefSpec{refSpec},
		})
	})
	if err == nil || errors.Is(err, goGit.NoErrAlreadyUpToDate) {
		slog.InfoContext(ctx, "Refreshed pinned ref",
			slog.String("ref", pinnedRef),
			slog.String("refspec", string(refSpec)),
		)
		return nil
	}
	return err
}

// Initializes a repository locally, at the given path.
// Adds the given remote repository as 'origin'.
// Then creates and pushes an empty commit.
func initRepo(ctx context.Context,
	dirPath,
	originURL string,
	authMethod transport.AuthMethod,
) *goGit.Repository {
	slog.InfoContext(ctx, "Detected remote repository is empty! Initializing repo locally")

	// Initialize repository locally.
	repo, err := goGit.PlainInitWithOptions(dirPath, &goGit.PlainInitOptions{
		InitOptions: goGit.InitOptions{
			DefaultBranch: plumbing.Main,
		},
	})
	assert.AssertErrNil(ctx, err, "Failed to initialize repo locally")

	// Add remote repository as 'origin'.
	_, err = repo.CreateRemote(&goGitConfig.RemoteConfig{
		Name: goGit.DefaultRemoteName,
		URLs: []string{originURL},
	})
	assert.AssertErrNil(ctx, err, "Failed adding 'origin' remote to the initilized local repo")

	// Create and push an empty commit.

	workTree, err := repo.Worktree()
	assert.AssertErrNil(ctx, err, "Failed getting repo worktree")

	author, attributedMessage := OperatorAttribution("chore : init")
	_, err = workTree.Commit(attributedMessage, &goGit.CommitOptions{
		Author:            author,
		AllowEmptyCommits: true,
	})
	assert.AssertErrNil(ctx, err, "Failed creating init git commit")

	releasePushTouch := requestTouchIfAuth(ctx,
		"push init commit to "+originShortName(repo), authMethod,
	)
	err = retryGitOperation(ctx, "push init commit", func() error {
		return repo.PushContext(ctx, &goGit.PushOptions{
			RemoteName: goGit.DefaultRemoteName,
			Auth:       authMethod,
			CABundle:   config.ParsedGeneralConfig.Git.CABundle,
		})
	})
	releasePushTouch()
	assert.AssertErrNil(ctx, err, "Failed pushing commit to upstream")

	return repo
}
