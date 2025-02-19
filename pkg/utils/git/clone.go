package git

import (
	"context"
	"log/slog"
	"os"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
	goGit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/transport"
)

// Clones the given git repository into the given directory (only if the repo doesn't already exist
// in there).
// If the repo already exists, then it checks out to the default branch and fetches the latest
// changes.
func CloneRepo(ctx context.Context,
	url, dirPath string,
	authMethod transport.AuthMethod,
) *goGit.Repository {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("repo", url), slog.String("dir", dirPath),
	})

	// If the repo already exists.
	if _, err := os.ReadDir(dirPath); err == nil {
		repo, err := goGit.PlainOpen(dirPath)
		assert.AssertErrNil(ctx, err, "Failed opening existing git repo")

		workTree, err := repo.Worktree()
		assert.AssertErrNil(ctx, err, "Failed getting repo worktree")

		// Checkout to default branch and fetch latest changes.
		// All changes in the current branch get discarded.
		CheckoutToDefaultBranch(ctx, repo, workTree, authMethod)

		return repo
	}

	// Clone git repo.

	slog.InfoContext(ctx, "Cloning repo")

	opts := &goGit.CloneOptions{
		Auth: authMethod,
		URL:  url,
	}
	if url == config.ParsedConfig.Forks.KubeaidForkURL {
		opts.Depth = 1
	}

	repo, err := goGit.PlainClone(dirPath, false, opts)
	assert.AssertErrNil(ctx, err, "Failed cloning repo")
	return repo
}
