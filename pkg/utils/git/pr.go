// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package git

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"os"
	"time"

	goGit "github.com/go-git/go-git/v5"
	goGitConfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/progress"
)

func AddCommitAndPushChanges(ctx context.Context,
	repo *goGit.Repository,
	workTree *goGit.Worktree,
	branch string,
	authMethod transport.AuthMethod,
	clusterName string,
	commitMessage string,
) plumbing.Hash {
	kubeaidConfigFork := config.ParsedGeneralConfig.Forks.KubeaidConfigFork

	err := workTree.AddGlob(fmt.Sprintf("k8s/%s/*", kubeaidConfigFork.Directory))
	assert.AssertErrNil(ctx, err, "Failed adding changes to git")

	status, err := workTree.Status()
	assert.AssertErrNil(ctx, err, "Failed determining git status")
	slog.InfoContext(ctx, "Determined git status", slog.Any("git-status", status))

	commit, err := workTree.Commit(commitMessage, &goGit.CommitOptions{
		Author: &object.Signature{
			Name:  "KubeAid Bootstrap Script",
			Email: "info@obmondo.com",
			When:  time.Now(),
		},
		AllowEmptyCommits: true,
	})
	assert.AssertErrNil(ctx, err, "Failed creating git commit")

	commitObject, err := repo.CommitObject(commit)
	assert.AssertErrNil(ctx, err, "Failed getting commit object")

	releasePushTouch := progress.FromCtx(ctx).RequestYubiKeyTouch(
		"push branch to " + originShortName(repo),
	)
	err = retryGitOperation(ctx, "push branch to origin", func() error {
		return repo.PushContext(ctx, &goGit.PushOptions{
			RemoteName: "origin",
			Auth:       authMethod,
			CABundle:   config.ParsedGeneralConfig.Git.CABundle,
			RefSpecs: []goGitConfig.RefSpec{
				goGitConfig.RefSpec("refs/heads/" + branch + ":refs/heads/" + branch),
			},
		})
	})
	releasePushTouch()
	assert.AssertErrNil(ctx, err, "Failed pushing commit to upstream")

	slog.InfoContext(ctx, "Added, committed and pushed changes",
		slog.String("commit-hash", commitObject.Hash.String()),
	)

	// When we didn't push the changes to the default branch, and rather to a feature branch,
	// log the create-PR URL so the operator has it in their bootstrap log. WaitUntilPRMerged
	// also surfaces it at the interactive prompt; the slog line here gives a permanent record.
	defaultBranchName := GetDefaultBranchName(ctx, authMethod, repo)
	if branch != defaultBranchName {
		slog.InfoContext(ctx, "Create and merge PR please",
			slog.String("URL", BuildPRCompareURL(repo, defaultBranchName, branch)))
	}

	return commitObject.Hash
}

// WaitUntilPRMerged blocks until the operator confirms via stdin that
// they've merged the feature branch into the default branch.
//
// The operator sees the PR URL (printed by AddCommitAndPushChanges
// before this is called), goes to their forge, merges, comes back, and
// presses ENTER. We then do ONE fetch + ONE commit-presence check to
// verify the merge actually happened — if it didn't (operator pressed
// ENTER too early, or merged the wrong branch), we say so and prompt
// again.
//
// Earlier this function polled via a 10-second fetch loop. That meant
// one YubiKey touch every 10 seconds for as long as the PR sat
// unmerged — easily 30+ touches if the operator took a few minutes.
// One touch per ENTER press is far better, and the operator is in
// control of when it fires.
//
// SkipPRWorkflow callers never reach this function — they push directly
// to the default branch. So this function always runs in interactive
// mode; no headless variant is needed.
func WaitUntilPRMerged(ctx context.Context,
	repo *goGit.Repository,
	defaultBranchName string,
	commitHash plumbing.Hash,
	auth transport.AuthMethod,
	branchToBeMerged string,
) {
	stdin := bufio.NewReader(os.Stdin)
	prURL := BuildPRCompareURL(repo, defaultBranchName, branchToBeMerged)

	for {
		slog.InfoContext(ctx, "Waiting for PR merge",
			slog.String("from-branch", branchToBeMerged),
			slog.String("to-branch", defaultBranchName),
		)
		fmt.Fprintf(os.Stderr, "\n→ Open and merge: %s\n", prURL)
		fmt.Fprintf(os.Stderr, "  Then press ENTER (Ctrl+C to abort): ")

		if err := readLineCtx(ctx, stdin); err != nil {
			assert.AssertErrNil(ctx, err, "Stopped waiting for PR merge")
		}

		releaseFetchTouch := progress.FromCtx(ctx).RequestYubiKeyTouch(
			"verify PR merge on " + originShortName(repo),
		)
		err := retryGitOperation(ctx, "fetch refs to verify PR merge", func() error {
			return repo.FetchContext(ctx, &goGit.FetchOptions{
				Auth:     auth,
				RefSpecs: []goGitConfig.RefSpec{"refs/*:refs/*"},
				CABundle: config.ParsedGeneralConfig.Git.CABundle,
			})
		})
		releaseFetchTouch()
		if err != nil && !errors.Is(err, goGit.NoErrAlreadyUpToDate) {
			assert.AssertErrNil(ctx, err, "Failed determining whether branch is merged or not")
		}

		defaultBranchRef, err := repo.Reference(
			plumbing.ReferenceName("refs/heads/"+defaultBranchName),
			true,
		)
		assert.AssertErrNil(ctx, err, "Failed to get default branch ref")

		if isCommitPresentInBranch(repo, commitHash, defaultBranchRef.Hash()) {
			slog.InfoContext(ctx, "Confirmed PR merged")
			return
		}

		fmt.Fprintf(os.Stderr,
			"  ✗ Commit %s isn't on %q yet. Merge the PR and try again.\n",
			commitHash.String()[:8], defaultBranchName,
		)
	}
}

// readLineCtx reads one line from r, but cancels and returns ctx.Err()
// if ctx is cancelled before the read completes (e.g., operator hit
// Ctrl+C). The blocked stdin read goroutine leaks on cancel — fine,
// the process is exiting.
func readLineCtx(ctx context.Context, r *bufio.Reader) error {
	done := make(chan error, 1)
	go func() {
		_, err := r.ReadString('\n')
		done <- err
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		return err
	}
}

func isCommitPresentInBranch(repo *goGit.Repository, commitHash, branchHash plumbing.Hash) bool {
	// Iterate through the commit history of the branch
	commits, err := repo.Log(&goGit.LogOptions{From: branchHash})
	if err != nil {
		log.Fatalf("Failed git logging : %v", err)
	}

	for {
		c, err := commits.Next()
		if err != nil {
			break
		}

		if c.Hash == commitHash {
			return true
		}
	}

	return false
}
