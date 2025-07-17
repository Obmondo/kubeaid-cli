package git

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"time"

	goGit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/google/go-github/v66/github"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
)

func GetDefaultBranchName(ctx context.Context,
	authMethod transport.AuthMethod,
	repo *goGit.Repository,
) string {
	remote, err := repo.Remote(goGit.DefaultRemoteName)
	assert.AssertErrNil(ctx, err, "Failed getting repo 'origin' remote")

	refs, err := remote.List(&goGit.ListOptions{
		Auth:     authMethod,
		CABundle: config.ParsedGeneralConfig.Git.CABundle,
	})
	assert.AssertErrNil(ctx, err, "Failed listing refs for 'origin' remote")

	for _, ref := range refs {
		if ref.Name() == plumbing.HEAD {
			target := ref.Target().String()

			defaultBranchName := target[11:] // Remove "refs/heads/" prefix.
			slog.InfoContext(ctx,
				"Detected default branch name",
				slog.String("branch", defaultBranchName),
			)

			return defaultBranchName
		}
	}

	panic("Failed detecting default branch name")
}

// Returns hostname of customer's git server.
func GetCustomerGitServerHostName(ctx context.Context) string {
	kubeaidConfigURL, err := url.Parse(config.ParsedGeneralConfig.Forks.KubeaidConfigForkURL)
	assert.AssertErrNil(ctx, err, "Failed parsing KubeAid config URL")

	return kubeaidConfigURL.Hostname()
}

// Returns latest tag of a git repo.
func GetLatestTag(ctx context.Context, repo *goGit.Repository, repoName string) string {
	tagIter, err := repo.Tags()
	assert.AssertErrNil(ctx, err, fmt.Sprintf("Failed getting tags for repo %s", repoName))
	var latestTagCommitTime time.Time
	var latestTag string
	err = tagIter.ForEach(func(r *plumbing.Reference) error {
		// Get the commit hash for the tag
		hash, err := repo.ResolveRevision(plumbing.Revision(r.Hash().String()))
		if err != nil {
			return err
		}
		// Get commit object
		commit, err := repo.CommitObject(*hash)
		if err != nil {
			return err
		}

		// Check if the commit is more recent than the current latest tag's commit
		if latestTag == "" || commit.Committer.When.After(latestTagCommitTime) {
			latestTag = r.Name().Short()
			latestTagCommitTime = commit.Committer.When
		}
		return nil
	})
	assert.AssertErrNil(ctx, err, fmt.Sprintf("Failed getting latest tag for repo %s", repoName))
	return latestTag
}

func GetLatestTagFromObmondoKubeAid(ctx context.Context) string {
	gitClient := github.NewClient(nil)
	tags, _, err := gitClient.Repositories.ListTags(ctx, "Obmondo", "KubeAid", nil)
	assert.AssertErrNil(ctx, err, "Failed getting latest tag from Obmondo KubeAid repo")

	latestTagCommitTime := tags[0].GetCommit().GetCommitter().GetDate().Time
	latestTag := tags[0].Name
	for _, tag := range tags {
		if tag.GetCommit().GetCommitter().GetDate().Time.After(latestTagCommitTime) {
			latestTagCommitTime = tag.GetCommit().GetCommitter().GetDate().Time
			latestTag = tag.Name
		}
	}
	return *latestTag
}
