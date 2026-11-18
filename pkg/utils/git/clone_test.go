// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package git

import (
	"context"
	"log/slog"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
)

func TestCloneRepoRejectsAnUnparsableURL(t *testing.T) {
	repo, err := New(nil).CloneRepo(context.Background(), "not a git URL", nil)

	assert.Nil(t, repo)
	assertGitFailure(t, err, "Failed parsing Git repository URL", slog.String("url", "not a git URL"))
}

func TestAddCommitAndPushChanges(t *testing.T) {
	ctx := context.Background()

	client := New(&config.Config{
		General: &config.GeneralConfig{
			Forks: config.ForksConfig{
				KubeaidConfigFork: config.KubeaidConfigForkConfig{Directory: "democust01"},
			},
		},
	})

	t.Run("nothing to add under the cluster directory", func(t *testing.T) {
		repo, workTree := newRepoWithCommit(t)

		hash, err := client.AddCommitAndPushChanges(ctx,
			repo, workTree, "feature/demo", nil, "cluster1", "chore: demo", "master",
		)

		assertGitFailure(t, err, "Failed adding changes to git")
		assert.Equal(t, plumbing.ZeroHash, hash)
	})

	t.Run("skips the push when the cluster directory has no changes", func(t *testing.T) {
		repo, workTree := newRepoWithCommit(t)
		commitFile(t, workTree, "k8s/democust01/values.yaml", "replicas: 1\n")

		hash, err := client.AddCommitAndPushChanges(ctx,
			repo, workTree, "feature/demo", nil, "cluster1", "chore: demo", "master",
		)

		require.NoError(t, err)
		assert.Equal(t, plumbing.ZeroHash, hash)
	})
}
