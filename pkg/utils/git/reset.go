// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package git

import (
	"context"
	"log/slog"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
)

func HardResetRepoToRef(ctx context.Context, repo *git.Repository, ref string) {
	if ref == "" || ref == plumbing.HEAD.String() {
		return
	}

	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("ref", ref),
	})

	slog.InfoContext(ctx, "Hard resetting repo to git ref")

	workTree, err := repo.Worktree()
	assert.AssertErrNil(ctx, err, "Failed getting repo worktree")

	targetCommitHash, err := resolveGitRefToCommitHash(repo, ref)
	assert.AssertErrNil(ctx, err,
		"Failed resolving provided git ref",
	)

	/*
		workTree.Reset errors out when we try to checkout to tag 20.1.1 for Obmondo's KubeAid, with
		the following message :

		  open /tmp/kubeaid-core/github.com/TheKilroyGroup/k8id/argocd-helm-charts/cert-manager/readme.md:
		  no such file or directory repo=https://github.com/TheKilroyGroup/k8id
	*/
	err = workTree.Checkout(&git.CheckoutOptions{
		Hash:  targetCommitHash,
		Force: true,
		Keep:  false,
	})
	assert.AssertErrNil(ctx, err, "Failed hard resetting to git ref")
}

func resolveGitRefToCommitHash(repo *git.Repository, ref string) (plumbing.Hash, error) {
	revision := plumbing.Revision(ref)
	targetCommitHash, err := repo.ResolveRevision(revision)
	if err != nil {
		return plumbing.ZeroHash, err
	}

	return *targetCommitHash, nil
}
