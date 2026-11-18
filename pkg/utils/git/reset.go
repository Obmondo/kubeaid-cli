// Copyright 2025 Obmondo
// SPDX-License-Identifier: Apache-2.0

package git

import (
	"context"
	"log/slog"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"

	"github.com/Obmondo/kubeaid-cli/pkg/utils/logger"
)

func HardResetRepoToRef(ctx context.Context, repo *git.Repository, ref string) (err error) {
	if ref == "" || ref == plumbing.HEAD.String() {
		return nil
	}

	logAttrs := []slog.Attr{slog.String("ref", ref)}
	ctx = logger.AppendSlogAttributesToCtx(ctx, logAttrs)
	defer annotate(&err, logAttrs...)

	slog.InfoContext(ctx, "Hard resetting repo to git ref")

	workTree, err := repo.Worktree()
	if err != nil {
		return fail("Failed getting repo worktree", err)
	}

	targetCommitHash, err := resolveGitRefToCommitHash(repo, ref)
	if err != nil {
		return fail("Failed resolving provided git ref", err)
	}

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
	if err != nil {
		return fail("Failed hard resetting to git ref", err)
	}

	return nil
}

func resolveGitRefToCommitHash(repo *git.Repository, ref string) (plumbing.Hash, error) {
	revision := plumbing.Revision(ref)
	targetCommitHash, err := repo.ResolveRevision(revision)
	if err != nil {
		return plumbing.ZeroHash, err
	}

	return *targetCommitHash, nil
}
