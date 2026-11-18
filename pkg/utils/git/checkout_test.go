// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package git

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	goGit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newRepoWithCommit returns a repo holding one commit of README.md on its default branch.
func newRepoWithCommit(t *testing.T) (*goGit.Repository, *goGit.Worktree) {
	t.Helper()

	repo := newTestRepo(t)

	workTree, err := repo.Worktree()
	require.NoError(t, err)

	commitFile(t, workTree, "README.md", "base\n")

	return repo, workTree
}

// commitFile writes contents to the file at path, which is relative to the worktree root, and
// commits it.
func commitFile(t *testing.T, workTree *goGit.Worktree, path, contents string) {
	t.Helper()

	filePath := filepath.Join(workTree.Filesystem.Root(), path)
	require.NoError(t, os.MkdirAll(filepath.Dir(filePath), 0o750))
	require.NoError(t, os.WriteFile(filePath, []byte(contents), 0o600))

	_, err := workTree.Add(path)
	require.NoError(t, err)

	_, err = workTree.Commit("commit "+path, &goGit.CommitOptions{
		Author: &object.Signature{Name: "Tester", Email: "tester@example.com", When: time.Now()},
	})
	require.NoError(t, err)
}

func TestCreateAndCheckoutToBranch(t *testing.T) {
	ctx := context.Background()
	defaultBranch := plumbing.Master.Short()

	t.Run("branches off the default branch and discards local changes", func(t *testing.T) {
		repo, workTree := newRepoWithCommit(t)
		SetRemoteHEADRef(ctx, repo, defaultBranch)

		readme := filepath.Join(workTree.Filesystem.Root(), "README.md")
		require.NoError(t, os.WriteFile(readme, []byte("dirty\n"), 0o600))

		require.NoError(t, New(nil).CreateAndCheckoutToBranch(ctx, repo, "feature/demo", workTree, nil))

		head, err := repo.Head()
		require.NoError(t, err)
		assert.Equal(t, plumbing.NewBranchReferenceName("feature/demo"), head.Name())

		contents, err := os.ReadFile(readme)
		require.NoError(t, err)
		assert.Equal(t, "base\n", string(contents))
	})

	t.Run("returns ErrBranchExists when the branch is already there", func(t *testing.T) {
		repo, workTree := newRepoWithCommit(t)
		SetRemoteHEADRef(ctx, repo, defaultBranch)

		client := New(nil)
		require.NoError(t, client.CreateAndCheckoutToBranch(ctx, repo, "feature/demo", workTree, nil))

		err := client.CreateAndCheckoutToBranch(ctx, repo, "feature/demo", workTree, nil)
		require.ErrorIs(t, err, ErrBranchExists)
		assertGitFailure(t, err, "Branch already exists", slog.String("branch", "feature/demo"))
	})

	failures := []struct {
		name         string
		build        func(t *testing.T) (*goGit.Repository, *goGit.Worktree)
		wantHeadline string
	}{
		{
			name: "fails to find HEAD in an empty repo",
			build: func(t *testing.T) (*goGit.Repository, *goGit.Worktree) {
				repo := newTestRepo(t)
				workTree, err := repo.Worktree()
				require.NoError(t, err)
				return repo, workTree
			},
			wantHeadline: "Failed getting head ref",
		},
		{
			name:         "fails to find the default branch without an origin remote",
			build:        newRepoWithCommit,
			wantHeadline: "Failed getting repo 'origin' remote",
		},
	}

	for _, tc := range failures {
		t.Run(tc.name, func(t *testing.T) {
			repo, workTree := tc.build(t)

			err := New(nil).CreateAndCheckoutToBranch(ctx, repo, "feature/demo", workTree, nil)
			assertGitFailure(t, err, tc.wantHeadline, slog.String("branch", "feature/demo"))
		})
	}
}
