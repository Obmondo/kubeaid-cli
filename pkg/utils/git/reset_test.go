// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package git

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	goGit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/stretchr/testify/require"
)

func TestResolveGitRefToCommitHash(t *testing.T) {
	repo, baseCommitHash, featureCommitHash := createTestRepoWithTagAndBranch(t)

	testCases := []struct {
		name       string
		ref        string
		wantCommit plumbing.Hash
	}{
		{
			name:       "tag",
			ref:        "v1.2.3",
			wantCommit: baseCommitHash,
		},
		{
			name:       "branch",
			ref:        "feature/test-branch",
			wantCommit: featureCommitHash,
		},
		{
			name:       "commit hash",
			ref:        baseCommitHash.String(),
			wantCommit: baseCommitHash,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotCommit, err := resolveGitRefToCommitHash(repo, tc.ref)
			require.NoError(t, err)
			require.Equal(t, tc.wantCommit, gotCommit)
		})
	}
}

func createTestRepoWithTagAndBranch(t *testing.T) (*goGit.Repository, plumbing.Hash, plumbing.Hash) {
	t.Helper()

	repoDir := t.TempDir()

	repo, err := goGit.PlainInit(repoDir, false)
	require.NoError(t, err)

	workTree, err := repo.Worktree()
	require.NoError(t, err)

	filePath := filepath.Join(repoDir, "README.md")

	err = os.WriteFile(filePath, []byte("base\n"), 0o600)
	require.NoError(t, err)

	_, err = workTree.Add("README.md")
	require.NoError(t, err)

	baseCommitHash, err := workTree.Commit("base commit", &goGit.CommitOptions{
		Author: &object.Signature{
			Name:  "Tester",
			Email: "tester@example.com",
			When:  time.Now(),
		},
	})
	require.NoError(t, err)

	_, err = repo.CreateTag("v1.2.3", baseCommitHash, nil)
	require.NoError(t, err)

	err = workTree.Checkout(&goGit.CheckoutOptions{
		Branch: plumbing.ReferenceName("refs/heads/feature/test-branch"),
		Create: true,
	})
	require.NoError(t, err)

	err = os.WriteFile(filePath, []byte("feature\n"), 0o600)
	require.NoError(t, err)

	_, err = workTree.Add("README.md")
	require.NoError(t, err)

	featureCommitHash, err := workTree.Commit("feature commit", &goGit.CommitOptions{
		Author: &object.Signature{
			Name:  "Tester",
			Email: "tester@example.com",
			When:  time.Now(),
		},
	})
	require.NoError(t, err)

	return repo, baseCommitHash, featureCommitHash
}
