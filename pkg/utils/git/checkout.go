// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package git

import (
	"context"
	"errors"
	"log/slog"
	"os"

	goGit "github.com/go-git/go-git/v5"
	gitConfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
)

// Removes any unstaged changes in the current branch. Then checks out to the default branch and
// fetches updates for that branch.
//
// Used by CloneRepo's re-run path for repos where kubeaid-cli walks
// history on the default branch (kubeaid-config — WaitUntilPRMerged's
// isCommitPresentInBranch needs the default branch's commit graph).
// Repos that are read-only-at-a-pinned-ref (kubeaid-fork) take the
// refreshPinnedRef path instead.
//
// The fetch is scoped to '+refs/heads/<defaultBranch>:refs/heads/<defaultBranch>'
// — narrow refspec, no tags. The previous '+refs/*:refs/*' + AllTags
// pulled every ref + every tag from the operator's kubeaid-config on
// every re-run, which is wasteful (we only check commit presence on
// the default branch) and turns out to be brittle: when the operator
// merges a PR with auto-delete-on-merge, refs/heads/<feature-branch>
// is gone remotely, and a wildcard refspec then surfaces
// "couldn't find remote ref" / "some refs were not updated" errors
// even though our actual target — the default branch — fetched fine.
//
// CreateAndCheckoutToBranch deliberately does NOT call this — callers
// there go through CloneRepo first, so the fetch has already happened;
// double-fetching back-to-back would just burn a YubiKey touch.
func CheckoutToDefaultBranchAndFetchUpdates(ctx context.Context,
	repo *goGit.Repository,
	workTree *goGit.Worktree,
	authMethod transport.AuthMethod,
) {
	checkoutDefaultBranch(ctx, repo, workTree, authMethod)

	defaultBranchName := GetDefaultBranchName(ctx, authMethod, repo)
	defaultBranchRefSpec := gitConfig.RefSpec(
		"+refs/heads/" + defaultBranchName + ":refs/heads/" + defaultBranchName,
	)

	releaseFetchTouch := requestTouchIfAuth(ctx,
		"fetch updates from "+originShortName(repo), authMethod,
	)
	err := retryGitOperation(ctx, "fetch latest changes", func() error {
		return repo.FetchContext(ctx, &goGit.FetchOptions{
			Auth:     authMethod,
			CABundle: config.ParsedGeneralConfig.Git.CABundle,
			RefSpecs: []gitConfig.RefSpec{defaultBranchRefSpec},
		})
	})
	releaseFetchTouch()
	if err != nil && !errors.Is(err, goGit.NoErrAlreadyUpToDate) {
		assert.AssertErrNil(ctx, err, "Failed fetching latest changes")
	}
	slog.InfoContext(ctx, "Fetched latest changes")
}

// checkoutDefaultBranch removes unstaged changes and checks out the
// default branch. NO network — relies on local refs being current. Use
// when the caller knows the repo was just fetched (e.g., immediately
// after CloneRepo's re-run path) so a second fetch would be redundant
// and would burn a YubiKey touch for nothing.
func checkoutDefaultBranch(ctx context.Context,
	repo *goGit.Repository,
	workTree *goGit.Worktree,
	authMethod transport.AuthMethod,
) {
	removeUnstagedChanges(ctx, repo, workTree)

	defaultBranchName := GetDefaultBranchName(ctx, authMethod, repo)
	err := workTree.Checkout(&goGit.CheckoutOptions{
		Branch: plumbing.ReferenceName("refs/heads/" + defaultBranchName),
		Keep:   false,
	})
	assert.AssertErrNil(ctx, err, "Failed checking out to default branch first")
	slog.InfoContext(ctx, "Checked out to the default branch")
}

// Discards all the changes in the current branch and checks out to the default branch first. Then,
// checks out to a new branhc with the given name.
// If a branch with that name already exists, then panics.
func CreateAndCheckoutToBranch(ctx context.Context,
	repo *goGit.Repository,
	branch string,
	workTree *goGit.Worktree,
	authMethod transport.AuthMethod,
) {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("branch", branch),
	})

	// Checkout to default branch (no fetch — CloneRepo's re-run path
	// already fetched). All changes in the current branch get discarded.
	checkoutDefaultBranch(ctx, repo, workTree, authMethod)

	// Error out if the branch already exists.
	branchRef, err := repo.Reference(plumbing.ReferenceName("refs/heads/"+branch), true)
	if err == nil && branchRef != nil {
		slog.ErrorContext(ctx, "Branch already exists")
		os.Exit(1)
	}

	err = workTree.Checkout(&goGit.CheckoutOptions{
		Branch: plumbing.ReferenceName("refs/heads/" + branch),
		Keep:   false,
		Create: true,
	})
	assert.AssertErrNil(ctx, err, "Failed creating and checking out to branch")

	slog.InfoContext(ctx, "Created and checked out to new branch")
}

// Removes any unstaged changes in the current branch, by hard resetting to the latest commit in
// that branch.
// Otherwise, we'll get error when checking out to a new branch.
func removeUnstagedChanges(ctx context.Context, repo *goGit.Repository, workTree *goGit.Worktree) {
	headRef, err := repo.Head()
	assert.AssertErrNil(ctx, err, "Failed getting head ref")

	err = workTree.Reset(&goGit.ResetOptions{
		Commit: headRef.Hash(),
		Mode:   goGit.HardReset,
	})
	assert.AssertErrNil(ctx, err, "Failed hard resetting to latest commit")
}
