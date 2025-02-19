package git

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	goGit "github.com/go-git/go-git/v5"
	gitConfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
)

func AddCommitAndPushChanges(ctx context.Context,
	repo *goGit.Repository,
	workTree *goGit.Worktree,
	branch string,
	auth transport.AuthMethod,
	clusterName string,
	commitMessage string,
) plumbing.Hash {
	err := workTree.AddGlob(fmt.Sprintf("k8s/%s/*", config.ParsedConfig.Cluster.Name))
	assert.AssertErrNil(ctx, err, "Failed adding changes to git")

	status, err := workTree.Status()
	assert.AssertErrNil(ctx, err, "Failed determining git status")
	slog.Info("Determined git status", slog.Any("git-status", status))

	commit, err := workTree.Commit(commitMessage, &goGit.CommitOptions{
		Author: &object.Signature{
			Name:  "KubeAid Bootstrap Script",
			Email: "info@obmondo.com",
			When:  time.Now(),
		},
	})
	assert.AssertErrNil(ctx, err, "Failed creating git commit")

	commitObject, err := repo.CommitObject(commit)
	assert.AssertErrNil(ctx, err, "Failed getting commit object")

	err = repo.Push(&goGit.PushOptions{
		Progress:   os.Stdout,
		RemoteName: "origin",
		RefSpecs: []gitConfig.RefSpec{
			gitConfig.RefSpec("refs/heads/" + branch + ":refs/heads/" + branch),
		},
		Auth: auth,
	})
	assert.AssertErrNil(ctx, err, "Failed pushing commit to upstream")

	slog.Info("Added, committed and pushed changes", slog.String("commit-hash", commitObject.Hash.String()))
	slog.Info("Create and merge PR please", slog.String("URL", getCreatePRURL(branch)))

	return commitObject.Hash
}

func getCreatePRURL(fromBranch string) string {
	var (
		parts = strings.Split(config.ParsedConfig.Forks.KubeaidConfigForkURL, "/")

		repoOwner = parts[len(parts)-2]
		repoName  = strings.Split(parts[len(parts)-1], ".git")[0]
	)

	createPRURL := fmt.Sprintf("%s/compare/main...%s:%s:%s",
		config.ParsedConfig.Forks.KubeaidConfigForkURL, repoOwner, repoName, fromBranch)

	return createPRURL
}

// TODO : Sometimes we get this error while trying to detect whether the branch has been merged
// or not : `unexpected EOF`. In that case, just retry instead of erroring out.
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
			RefSpecs: []gitConfig.RefSpec{"refs/*:refs/*"},
		})
		if !errors.Is(err, goGit.NoErrAlreadyUpToDate) {
			assert.AssertErrNil(ctx, err, "Failed determining whether branch is merged or not")
		}

		defaultBranchRef, err := repo.Reference(plumbing.ReferenceName("refs/heads/"+defaultBranchName), true)
		assert.AssertErrNil(ctx, err, "Failed to get default branch ref")

		if commitPresent := isCommitPresentInBranch(repo, commitHash, defaultBranchRef.Hash()); commitPresent {
			slog.Info("Detected branch merge")
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
