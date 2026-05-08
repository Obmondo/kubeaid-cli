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
// fetches all updates.
//
// Used by CloneRepo's re-run path to reset an already-cloned repo to a
// known clean state. CreateAndCheckoutToBranch deliberately does NOT
// call this — callers there go through CloneRepo first, so the fetch
// has already happened; double-fetching back-to-back would just burn
// a YubiKey touch.
func CheckoutToDefaultBranchAndFetchUpdates(ctx context.Context,
	repo *goGit.Repository,
	workTree *goGit.Worktree,
	authMethod transport.AuthMethod,
) {
	checkoutDefaultBranch(ctx, repo, workTree, authMethod)

	// Fetch all the changes.
	releaseFetchTouch := requestTouchIfAuth(ctx,
		"fetch updates from "+originShortName(repo), authMethod,
	)
	err := retryGitOperation(ctx, "fetch latest changes", func() error {
		return repo.FetchContext(ctx, &goGit.FetchOptions{
			Auth:     authMethod,
			CABundle: config.ParsedGeneralConfig.Git.CABundle,

			// The '+' tells Git to update the reference even if it is not a fast-forward,
			// like history rewrites.
			RefSpecs: []gitConfig.RefSpec{"+refs/*:refs/*"},

			Tags: goGit.AllTags,
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
