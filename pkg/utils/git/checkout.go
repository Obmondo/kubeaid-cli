// Copyright 2025 Obmondo
// SPDX-License-Identifier: Apache-2.0

package git

import (
	"context"
	"errors"
	"log/slog"

	goGit "github.com/go-git/go-git/v5"
	gitConfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"

	"github.com/Obmondo/kubeaid-cli/pkg/utils/logger"
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
func (c *Client) CheckoutToDefaultBranchAndFetchUpdates(ctx context.Context,
	repo *goGit.Repository,
	workTree *goGit.Worktree,
	authMethod transport.AuthMethod,
) error {
	if err := c.checkoutDefaultBranch(ctx, repo, workTree, authMethod); err != nil {
		return err
	}

	defaultBranchName, err := c.GetDefaultBranchName(ctx, authMethod, repo)
	if err != nil {
		return err
	}
	defaultBranchRefSpec := gitConfig.RefSpec(
		"+refs/heads/" + defaultBranchName + ":refs/heads/" + defaultBranchName,
	)

	releaseFetchTouch := requestTouchIfAuth(ctx,
		"fetch updates from "+originShortName(repo), authMethod,
	)
	err = retryGitOperation(ctx, "fetch latest changes", func() error {
		return repo.FetchContext(ctx, &goGit.FetchOptions{
			Auth:     authMethod,
			CABundle: c.caBundle(),
			RefSpecs: []gitConfig.RefSpec{defaultBranchRefSpec},
		})
	})
	releaseFetchTouch()
	if err != nil && !errors.Is(err, goGit.NoErrAlreadyUpToDate) {
		return fail("Failed fetching latest changes", err)
	}
	slog.InfoContext(ctx, "Fetched latest changes")

	return nil
}

// checkoutDefaultBranch removes unstaged changes and checks out the
// default branch. NO network — relies on local refs being current. Use
// when the caller knows the repo was just fetched (e.g., immediately
// after CloneRepo's re-run path) so a second fetch would be redundant
// and would burn a YubiKey touch for nothing.
func (c *Client) checkoutDefaultBranch(ctx context.Context,
	repo *goGit.Repository,
	workTree *goGit.Worktree,
	authMethod transport.AuthMethod,
) error {
	if err := removeUnstagedChanges(repo, workTree); err != nil {
		return err
	}

	defaultBranchName, err := c.GetDefaultBranchName(ctx, authMethod, repo)
	if err != nil {
		return err
	}

	err = workTree.Checkout(&goGit.CheckoutOptions{
		Branch: plumbing.ReferenceName("refs/heads/" + defaultBranchName),
		Keep:   false,
	})
	if err != nil {
		return fail("Failed checking out to default branch first", err)
	}
	slog.InfoContext(ctx, "Checked out to the default branch")

	return nil
}

// Discards all the changes in the current branch and checks out to the default branch first. Then,
// checks out to a new branhc with the given name.
// If a branch with that name already exists, then returns ErrBranchExists.
func (c *Client) CreateAndCheckoutToBranch(ctx context.Context,
	repo *goGit.Repository,
	branch string,
	workTree *goGit.Worktree,
	authMethod transport.AuthMethod,
) (err error) {
	logAttrs := []slog.Attr{slog.String("branch", branch)}
	ctx = logger.AppendSlogAttributesToCtx(ctx, logAttrs)
	defer annotate(&err, logAttrs...)

	// Checkout to default branch (no fetch — CloneRepo's re-run path
	// already fetched). All changes in the current branch get discarded.
	if err := c.checkoutDefaultBranch(ctx, repo, workTree, authMethod); err != nil {
		return err
	}

	// Error out if the branch already exists.
	branchRef, err := repo.Reference(plumbing.ReferenceName("refs/heads/"+branch), true)
	if err == nil && branchRef != nil {
		return fail("Branch already exists", ErrBranchExists)
	}

	err = workTree.Checkout(&goGit.CheckoutOptions{
		Branch: plumbing.ReferenceName("refs/heads/" + branch),
		Keep:   false,
		Create: true,
	})
	if err != nil {
		return fail("Failed creating and checking out to branch", err)
	}

	slog.InfoContext(ctx, "Created and checked out to new branch")

	return nil
}

// Removes any unstaged changes in the current branch, by hard resetting to the latest commit in
// that branch.
// Otherwise, we'll get error when checking out to a new branch.
func removeUnstagedChanges(repo *goGit.Repository, workTree *goGit.Worktree) error {
	headRef, err := repo.Head()
	if err != nil {
		return fail("Failed getting head ref", err)
	}

	err = workTree.Reset(&goGit.ResetOptions{
		Commit: headRef.Hash(),
		Mode:   goGit.HardReset,
	})
	if err != nil {
		return fail("Failed hard resetting to latest commit", err)
	}

	return nil
}
