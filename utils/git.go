package utils

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/Obmondo/kubeaid-bootstrap-script/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/utils/logger"
	"github.com/go-git/go-git/v5"
	gitConfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
)

func GetGitAuthMethod(ctx context.Context) (authMethod transport.AuthMethod) {
	slog.InfoContext(ctx, "Determining git auth method")

	var err error

	switch {
	// SSH private key and password.
	case len(config.ParsedConfig.Git.SSHPrivateKey) > 0:
		authMethod, err = ssh.NewPublicKeysFromFile("git", config.ParsedConfig.Git.SSHPrivateKey, config.ParsedConfig.Git.Password)
		assert.AssertErrNil(ctx, err,
			"Failed generating SSH public key from SSH private key and password for git",
		)
		slog.Info("Using SSH private key and password")

	// Username and password.
	case len(config.ParsedConfig.Git.Password) > 0:
		authMethod = &http.BasicAuth{
			Username: config.ParsedConfig.Git.Username,
			Password: config.ParsedConfig.Git.Password,
		}
		slog.Info("Using username and password")

	// SSH agent.
	default:
		authMethod, err = ssh.NewSSHAgentAuth("git")
		assert.AssertErrNil(ctx, err, "SSH agent failed")
		slog.Info("Using SSH agent")
	}
	return
}

// Clones the given git repository into the given directory (only if the repo doesn't already exist
// in there).
// If the repo already exists, then it checks out to the default branch and fetches the latest
// changes.
func GitCloneRepo(ctx context.Context,
	url, dirPath string,
	authMethod transport.AuthMethod,
) *git.Repository {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("repo", url), slog.String("dir", dirPath),
	})

	// If the repo already exists.
	if _, err := os.ReadDir(dirPath); err == nil {
		repo, err := git.PlainOpen(dirPath)
		assert.AssertErrNil(ctx, err, "Failed opening existing git repo")

		workTree, err := repo.Worktree()
		assert.AssertErrNil(ctx, err, "Failed getting repo worktree")

		// Checkout to the default branch.
		defaultBranchName := GetDefaultBranchName(ctx, repo)
		err = workTree.Checkout(&git.CheckoutOptions{
			Branch: plumbing.ReferenceName("refs/heads/" + defaultBranchName),
			Keep:   false,
		})
		assert.AssertErrNil(ctx, err, "Failed checking out to default branch first")
		slog.InfoContext(ctx, "Checked out to the default branch")

		// Fetch all the changes.
		err = repo.Fetch(&git.FetchOptions{
			Auth:     authMethod,
			RefSpecs: []gitConfig.RefSpec{"refs/*:refs/*"},
		})
		if !errors.Is(err, git.NoErrAlreadyUpToDate) {
			assert.AssertErrNil(ctx, err, "Failed fetching latest changes")
		}
		slog.InfoContext(ctx, "Fetched latest changes")

		return repo
	}

	// Clone git repo.

	slog.InfoContext(ctx, "Cloning repo")

	opts := &git.CloneOptions{
		Auth: authMethod,
		URL:  url,
	}
	if url == config.ParsedConfig.Forks.KubeaidForkURL {
		opts.Depth = 1
	}

	repo, err := git.PlainClone(dirPath, false, opts)
	assert.AssertErrNil(ctx, err, "Failed cloning repo")
	return repo
}

func GetDefaultBranchName(ctx context.Context, repo *git.Repository) string {
	remote, err := repo.Remote("origin")
	assert.AssertErrNil(ctx, err, "Failed getting repo 'origin' remote")

	refs, err := remote.List(&git.ListOptions{})
	assert.AssertErrNil(ctx, err, "Failed listing refs for 'origin' remote")

	for _, ref := range refs {
		if ref.Name().String() == "HEAD" {
			target := ref.Target().String()

			defaultBranchName := target[11:] // Remove "refs/heads/" prefix.
			slog.InfoContext(ctx, "Detected default branch name", slog.String("branch", defaultBranchName))

			return defaultBranchName
		}
	}

	panic("Failed detecting default branch name")
}

func CreateAndCheckoutToBranch(ctx context.Context,
	repo *git.Repository,
	branch string,
	workTree *git.Worktree,
) {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("branch", branch),
	})

	// Check if the branch already exists.
	branchRef, err := repo.Reference(plumbing.ReferenceName("refs/heads/"+branch), true)
	if err == nil && branchRef != nil {
		slog.ErrorContext(ctx, "Branch already exists")
		os.Exit(1)
	}

	err = workTree.Checkout(&git.CheckoutOptions{
		Branch: plumbing.ReferenceName("refs/heads/" + branch),
		Keep:   false,
		Create: true,
	})
	assert.AssertErrNil(ctx, err, "Failed creating and checking out to branch")

	slog.InfoContext(ctx, "Created and checked out to new branch")
}

func AddCommitAndPushChanges(ctx context.Context,
	repo *git.Repository,
	workTree *git.Worktree,
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

	commit, err := workTree.Commit(commitMessage, &git.CommitOptions{
		Author: &object.Signature{
			Name:  "KubeAid Bootstrap Script",
			Email: "info@obmondo.com",
			When:  time.Now(),
		},
	})
	assert.AssertErrNil(ctx, err, "Failed creating git commit")

	commitObject, err := repo.CommitObject(commit)
	assert.AssertErrNil(ctx, err, "Failed getting commit object")

	err = repo.Push(&git.PushOptions{
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
// or not : `unexpected EOF`.
// In that case, just retry instead of erroring out.
func WaitUntilPRMerged(ctx context.Context,
	repo *git.Repository,
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

		err := repo.Fetch(&git.FetchOptions{
			Auth:     auth,
			RefSpecs: []gitConfig.RefSpec{"refs/*:refs/*"},
		})
		if !errors.Is(err, git.NoErrAlreadyUpToDate) {
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

func isCommitPresentInBranch(repo *git.Repository, commitHash, branchHash plumbing.Hash) bool {
	// Iterate through the commit history of the branch
	commits, err := repo.Log(&git.LogOptions{From: branchHash})
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
