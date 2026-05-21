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

	"github.com/charmbracelet/lipgloss"
	goGit "github.com/go-git/go-git/v5"
	goGitConfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/progress"
)

// defaultBranchName is the fork's default branch (e.g. "main") — passed
// in by the caller rather than re-discovered inside this function. The
// caller already called GetDefaultBranchName once during its own setup
// (setup_kubeaid_config.go / upgrade_cluster.go); passing the value
// through avoids a second remote.ListContext call right after the push,
// which over SSH triggers a separate "look up default branch on <fork>"
// YubiKey touch on every commit-push cycle.
func AddCommitAndPushChanges(ctx context.Context,
	repo *goGit.Repository,
	workTree *goGit.Worktree,
	branch string,
	authMethod transport.AuthMethod,
	clusterName string,
	commitMessage string,
	defaultBranchName string,
) plumbing.Hash {
	kubeaidConfigFork := config.ParsedGeneralConfig.Forks.KubeaidConfigFork

	err := workTree.AddGlob(fmt.Sprintf("k8s/%s/*", kubeaidConfigFork.Directory))
	assert.AssertErrNil(ctx, err, "Failed adding changes to git")

	status, err := workTree.Status()
	assert.AssertErrNil(ctx, err, "Failed determining git status")
	slog.InfoContext(ctx, "Determined git status", slog.Any("git-status", status))

	// Skip the whole commit-push-prompt-merge dance when nothing
	// actually changed. Without this guard, AllowEmptyCommits: true
	// would create an empty commit, push it, and surface a noop PR
	// for the operator to merge — exactly the diff-less PR they had
	// to manually close every re-run after our SealedSecret
	// idempotency landed. ZeroHash signals "nothing committed" to
	// the caller; SetupKubeAidConfig short-circuits past
	// WaitUntilPRMerged on that.
	if status.IsClean() {
		slog.InfoContext(ctx, "No changes to commit; skipping push + PR merge")
		return plumbing.ZeroHash
	}

	author, attributedMessage := OperatorAttribution(commitMessage)
	commit, err := workTree.Commit(attributedMessage, &goGit.CommitOptions{
		Author: author,
		Signer: CommitSigner(ctx),
		// AllowEmptyCommits stays false (the default) — the
		// IsClean() guard above is the user-facing check; this is
		// belt-and-braces in case status reports clean but Commit
		// disagrees on edge cases (mode-only changes, etc.).
		AllowEmptyCommits: false,
	})
	assert.AssertErrNil(ctx, err, "Failed creating git commit")

	commitObject, err := repo.CommitObject(commit)
	assert.AssertErrNil(ctx, err, "Failed getting commit object")

	releasePushTouch := requestTouchIfAuth(ctx,
		"push branch to "+originShortName(repo), authMethod,
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
	bar := progress.FromCtx(ctx)

	slog.InfoContext(ctx, "Waiting for PR merge",
		slog.String("from-branch", branchToBeMerged),
		slog.String("to-branch", defaultBranchName),
	)

	for {
		// Pause the bar so its 100ms auto-render goroutine can't
		// overwrite the prompt via `\r`. Save cursor at the cleared
		// spinner line; on success we restore-and-clear there to make
		// the whole prompt block disappear (auto-hide, same shape as
		// the YubiKey-touch erase).
		bar.Pause()
		fmt.Fprint(os.Stderr, "\033[s")
		fmt.Fprintln(os.Stderr, renderPRMergeBox(prURL))
		fmt.Fprint(os.Stderr, "> ")

		if err := readLineCtx(ctx, stdin); err != nil {
			assert.AssertErrNil(ctx, err, "Stopped waiting for PR merge")
		}

		// Erase the prompt block (restore cursor + clear to end of
		// screen) before the spinner resumes — keeps the transcript
		// clean if verify succeeds; if it fails we'll print an error
		// below and re-prompt.
		fmt.Fprint(os.Stderr, "\033[u\033[J")
		bar.Resume()

		releaseFetchTouch := requestTouchIfAuth(ctx,
			"verify PR merge on "+originShortName(repo), auth,
		)
		// Targeted refspec: only fetch the default branch, force-update
		// it locally. The previous "refs/*:refs/*" form tried to update
		// every local ref — including refs/heads/<feature-branch>, which
		// gets auto-deleted on the remote when the operator merges with
		// "delete branch after merge" enabled. With no '+' prefix, the
		// can't-update on the now-stale feature-branch ref made go-git
		// flag the WHOLE fetch as failed with "some refs were not
		// updated", even though refs/heads/<defaultBranch> updated fine.
		// Net effect: the operator merges the PR, kubeaid-cli kills
		// itself one second later. The targeted refspec sidesteps this:
		// we only need the default branch ref to check commit presence,
		// not anything else.
		err := retryGitOperation(ctx, "fetch refs to verify PR merge", func() error {
			return repo.FetchContext(ctx, &goGit.FetchOptions{
				Auth: auth,
				RefSpecs: []goGitConfig.RefSpec{
					goGitConfig.RefSpec(
						"+refs/heads/" + defaultBranchName + ":refs/heads/" + defaultBranchName,
					),
				},
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
			"   ✗ Commit %s isn't on %q yet. Merge the PR and try again.\n",
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

// renderPRMergeBox lays the PR-merge prompt out as a lipgloss bordered
// box with the same rounded-border style as the K8s profile picker and
// DNS-wait table — keeps the operator-facing surfaces visually
// consistent.
//
// No explicit Width: the box sizes to its longest content line, which
// keeps the PR URL on a single physical line. The terminal's own
// visual-wrap then takes over when the URL exceeds the viewport width
// — no \n is inserted mid-URL, so terminal-side URL detection sees one
// contiguous link and a mouse-click captures the whole thing. The
// previous Width(termWidth-2) version had lipgloss soft-wrap the URL,
// which inserted a real newline and broke the URL into two unclickable
// halves.
//
// If the operator splits the terminal narrower than the URL, the box
// border itself extends past the viewport — visually ugly but the URL
// stays clickable as a single target, which is what actually matters.
//
// The caller prints '> ' below the rendered box; that's where the
// operator's ENTER lands. On success the whole block (box + prompt
// row + typed input) is erased via the existing \033[u\033[J
// auto-hide.
func renderPRMergeBox(prURL string) string {
	headerStyle := lipgloss.NewStyle().Bold(true)
	urlStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("39")). // bright blue
		Underline(true)
	hintStyle := lipgloss.NewStyle().Faint(true)

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		headerStyle.Render("Open and merge in your browser:"),
		urlStyle.Render(prURL),
		"",
		hintStyle.Render("Press ENTER once merged  •  Ctrl+C to abort"),
	)
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Padding(0, 1)
	return boxStyle.Render(content)
}
