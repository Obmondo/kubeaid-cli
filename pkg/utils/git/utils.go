package git

import (
	"context"
	"log/slog"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	goGit "github.com/go-git/go-git/v5"
)

func GetDefaultBranchName(ctx context.Context, repo *goGit.Repository) string {
	remote, err := repo.Remote("origin")
	assert.AssertErrNil(ctx, err, "Failed getting repo 'origin' remote")

	refs, err := remote.List(&goGit.ListOptions{})
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
