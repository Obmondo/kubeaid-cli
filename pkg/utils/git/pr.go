// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package git

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"strings"
	"time"

	goGit "github.com/go-git/go-git/v5"
	goGitConfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
)

func AddCommitAndPushChanges(ctx context.Context,
	repo *goGit.Repository,
	workTree *goGit.Worktree,
	branch string,
	authMethod transport.AuthMethod,
	clusterName string,
	commitMessage string,
) plumbing.Hash {
	err := workTree.AddGlob(fmt.Sprintf(
		"k8s/%s/*", config.ParsedGeneralConfig.Forks.KubeaidConfigFork.Directory,
	))
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

	err = repo.Push(&goGit.PushOptions{
		RemoteName: "origin",
		Auth:       authMethod,
		CABundle:   config.ParsedGeneralConfig.Git.CABundle,
		RefSpecs: []goGitConfig.RefSpec{
			goGitConfig.RefSpec("refs/heads/" + branch + ":refs/heads/" + branch),
		},
	})
	assert.AssertErrNil(ctx, err, "Failed pushing commit to upstream")

	slog.InfoContext(ctx,
		"Added, committed and pushed changes",
		slog.String("commit-hash", commitObject.Hash.String()),
	)

	// If we didn't push the changes to the default branch, and rather to a feature branch,
	// then prompt the user to create a PR against and merge those changes into the default branch.
	defaultBranchName := GetDefaultBranchName(ctx, authMethod, repo)
	if branch != defaultBranchName {
		slog.InfoContext(ctx,
			"Create and merge PR please",
			slog.String("URL", getCreatePRURL(branch)),
		)
	}

	return commitObject.Hash
}

func getCreatePRURL(fromBranch string) string {
	var (
		parts = strings.Split(config.ParsedGeneralConfig.Forks.KubeaidConfigFork.URL, "/")

		repoOwner = parts[len(parts)-2]
		repoName  = strings.Split(parts[len(parts)-1], ".git")[0]
	)

	createPRURL := fmt.Sprintf("%s/compare/main...%s:%s:%s",
		config.ParsedGeneralConfig.Forks.KubeaidConfigFork.URL, repoOwner, repoName, fromBranch)

	return createPRURL
}

// TODO : Sometimes we get this error while trying to detect whether the branch has been merged
// or not : `unexpected EOF`. In that case, just retry instead of erroring out.
//
//nolint:godox
func WaitUntilPRMerged(ctx context.Context,
	repo *goGit.Repository,
	defaultBranchName string,
	commitHash plumbing.Hash,
	auth transport.AuthMethod,
	branchToBeMerged string,
) {
	for {
		slog.Info("Waiting for PR to be merged. Sleeping for 10 seconds....",
			slog.String("from-branch", branchToBeMerged),
			slog.String("to-branch", defaultBranchName),
		)
		time.Sleep(10 * time.Second)

		err := repo.Fetch(&goGit.FetchOptions{
			Auth:     auth,
			RefSpecs: []goGitConfig.RefSpec{"refs/*:refs/*"},
			CABundle: config.ParsedGeneralConfig.Git.CABundle,
		})
		if !errors.Is(err, goGit.NoErrAlreadyUpToDate) {
			assert.AssertErrNil(ctx, err, "Failed determining whether branch is merged or not")
		}

		defaultBranchRef, err := repo.Reference(
			plumbing.ReferenceName("refs/heads/"+defaultBranchName),
			true,
		)
		assert.AssertErrNil(ctx, err, "Failed to get default branch ref")

		if commitPresent := isCommitPresentInBranch(repo, commitHash, defaultBranchRef.Hash()); commitPresent {
			slog.Info("Detected branch merged")
			return
		}
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
