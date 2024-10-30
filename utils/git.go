package utils

import (
	"fmt"
	"log"
	"log/slog"
	"os"
	"time"

	"github.com/Obmondo/kubeaid-bootstrap-script/constants"
	"github.com/go-git/go-git/v5"
	gitConfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
)

func GetGitAuthMethod() (authMethod transport.AuthMethod) {
	if len(constants.ParsedConfig.Git.SSHPrivateKey) > 0 {
		publicKeys, err := ssh.NewPublicKeysFromFile("git", constants.ParsedConfig.Git.SSHPrivateKey, constants.ParsedConfig.Git.Password)
		if err != nil {
			log.Fatalf("Failed generating SSH public key from SSH private key and password for git : %v", err)
		}
		authMethod = publicKeys
		slog.Info("Using SSH private key and password for git authentication")
		return
	}

	if len(constants.ParsedConfig.Git.Password) > 0 {
		authMethod = &http.BasicAuth{
			Username: constants.ParsedConfig.Git.Username,
			Password: constants.ParsedConfig.Git.Password,
		}
		slog.Info("Using username and password for git authentication")
		return
	}

	sshAuth, err := ssh.NewSSHAgentAuth("git")
	if err != nil {
		log.Fatalf("SSH agent failed : %v", err)
	}
	authMethod = sshAuth
	slog.Info("Using SSH agent for git authentication")
	return
}

func GitCloneRepo(url, dir string, authMethod transport.AuthMethod) *git.Repository {
	opts := &git.CloneOptions{
		Auth: authMethod,
		URL:  url,
	}
	if url == constants.ParsedConfig.Forks.KubeaidForkURL {
		opts.Depth = 1
	}

	repo, err := git.PlainClone(dir, false, opts)
	if err != nil {
		log.Fatalf("Failed git cloning repo %s in %s : %v", url, dir, err)
	}
	slog.Info("Cloned repo", slog.String("repo", url), slog.String("dir", dir))
	return repo
}

func GetDefaultBranchName(repo *git.Repository) string {
	headRef, err := repo.Head()
	if err != nil {
		log.Fatal("Failed getting HEAD ref of kubeaid-config repo")
	}
	return headRef.Name().Short()
}

func CreateAndCheckoutToBranch(repo *git.Repository, branch string, workTree *git.Worktree) {
	// Check if the branch already exists.
	branchRef, err := repo.Reference(plumbing.ReferenceName("refs/heads/"+branch), true)
	if err == nil && branchRef != nil {
		log.Fatalf("Branch %s already exists", branch)
	}

	if err = workTree.Checkout(&git.CheckoutOptions{
		Branch: plumbing.ReferenceName("refs/heads/" + branch),
		Create: true,
	}); err != nil {
		log.Fatalf("Failed creating and checking out to branch %s : %v", branch, err)
	}
	slog.Info("Created and checked out to new branch", slog.String("branch", branch))
}

func AddCommitAndPushChanges(repo *git.Repository, workTree *git.Worktree, branch string, auth transport.AuthMethod, clusterName string, commitMessage string) plumbing.Hash {
	if err := workTree.AddGlob(fmt.Sprintf("k8s/%s/*", clusterName)); err != nil {
		log.Fatalf("Failed adding changes to git : %v", err)
	}

	status, err := workTree.Status()
	if err != nil {
		log.Fatalf("Failed determining git status : %v", err)
	}
	slog.Info("Determined git status", slog.Any("git-status", status))

	commit, err := workTree.Commit(commitMessage, &git.CommitOptions{
		Author: &object.Signature{
			Name:  "KubeAid Installer",
			Email: "info@obmondo.com",
			When:  time.Now(),
		},
	})
	if err != nil {
		log.Fatalf("Failed creating git commit : %v", err)
	}
	commitObject, err := repo.CommitObject(commit)
	if err != nil {
		log.Fatalf("Failed getting commit object : %v", err)
	}

	if err = repo.Push(&git.PushOptions{
		Progress:   os.Stdout,
		RemoteName: "origin",
		RefSpecs: []gitConfig.RefSpec{
			gitConfig.RefSpec("refs/heads/" + branch + ":refs/heads/" + branch),
		},
		Auth: auth,
	}); err != nil {
		log.Fatalf("git push failed : %v", err)
	}

	slog.Info("Added, committed and pushed changes", slog.String("commit-hash", commitObject.Hash.String()))
	return commitObject.Hash
}

func WaitUntilPRMerged(repo *git.Repository, defaultBranchName string, commitHash plumbing.Hash, auth transport.AuthMethod, branchToBeMerged string) {
	for {
		slog.Info("Waiting for %s branch to be merged into the default branch %s. Sleeping for 10 seconds...\n", branchToBeMerged, defaultBranchName)
		time.Sleep(10 * time.Second)

		if err := repo.Fetch(&git.FetchOptions{
			Auth:     auth,
			RefSpecs: []gitConfig.RefSpec{"refs/*:refs/*"},
		}); err != nil && err != git.NoErrAlreadyUpToDate {
			log.Fatalf("Failed determining whether branch is merged or not : %v", err)
		}

		defaultBranchRef, err := repo.Reference(plumbing.ReferenceName("refs/heads/"+defaultBranchName), true)
		if err != nil {
			log.Fatalf("Failed to get default branch ref of kubeaid-config repo : %v", err)
		}

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
