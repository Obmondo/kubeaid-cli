package git

import (
	"context"
	"log/slog"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
)

func HardResetRepoToTag(ctx context.Context, repo *git.Repository, tag string) {
	if tag == plumbing.HEAD.String() {
		return
	}

	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("tag", tag),
	})

	slog.InfoContext(ctx, "Hard resetting repo to tag")

	workTree, err := repo.Worktree()
	assert.AssertErrNil(ctx, err, "Failed getting repo worktree")

	tagReference, err := repo.Reference(plumbing.NewTagReferenceName(tag), true)
	assert.AssertErrNil(ctx, err,
		"Failed resolving reference for provided tag",
	)

	targetCommitHash := tagReference.Hash()

	tagObject, err := repo.TagObject(tagReference.Hash())
	if err == nil {
		// Resolve the tag reference hash to the tag object / corresponding commit hash.
		targetCommitHash = tagObject.Target
	}

	err = workTree.Reset(&git.ResetOptions{
		Commit: targetCommitHash,
		Mode:   git.HardReset,
	})
	assert.AssertErrNil(ctx, err, "Failed hard resetting repo to provided tag")
}
