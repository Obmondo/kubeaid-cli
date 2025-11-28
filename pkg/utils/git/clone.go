// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package git

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"time"

	goGit "github.com/go-git/go-git/v5"
	goGitConfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
)

// Clones the given git repository, if that isn't already done.
// When the repo is already cloned, it checks out to the default branch and fetches the latest
// changes.
func CloneRepo(ctx context.Context, url string, authMethod transport.AuthMethod) *goGit.Repository {
	// Determine the path, where this repository will be / is cloned.
	path := GetRepoDir(url)

	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("repo", url), slog.String("path", path),
	})

	// If the repo is already cloned.
	if _, err := os.ReadDir(path); err == nil {
		repo, err := goGit.PlainOpen(path)
		if err != nil && errors.Is(err, goGit.ErrRepositoryNotExists) {
			assert.AssertErrNil(ctx, err, "Failed opening existing git repo")
		}

		workTree, err := repo.Worktree()
		assert.AssertErrNil(ctx, err, "Failed getting repo worktree")

		// Checkout to default branch and fetch latest changes.
		// All changes in the current branch get discarded.
		CheckoutToDefaultBranchAndFetchUpdates(ctx, repo, workTree, authMethod)

		return repo
	}

	// Clone the repo.
	slog.InfoContext(ctx, "Cloning repo")

	opts := &goGit.CloneOptions{
		URL:      url,
		Auth:     nil,
		CABundle: config.ParsedGeneralConfig.Git.CABundle,
	}

	isPrivate, err := isRepoPrivate(ctx, url)
	assert.AssertErrNil(ctx, err, "Failed to determine repo visibility")
	if isPrivate {
		opts.Auth = authMethod
	}

	repo, err := goGit.PlainClone(path, false, opts)
	if errors.Is(err, transport.ErrEmptyRemoteRepository) &&
		(url == config.ParsedGeneralConfig.Forks.KubeaidConfigForkURL) {
		// Remote KubeAid Config repository is empty.
		// So, we need to initialize the repository locally,
		// add the remote repository as 'origin',
		// and create and push an empty commit.
		repo = initRepo(ctx,
			path,
			config.ParsedGeneralConfig.Forks.KubeaidConfigForkURL,
			authMethod,
		)
	} else {
		assert.AssertErrNil(ctx, err, "Failed cloning repo")
	}
	return repo
}

// Initializes a repository locally, at the given path.
// Adds the given remote repository as 'origin'.
// Then creates and pushes an empty commit.
func initRepo(ctx context.Context,
	dirPath,
	originURL string,
	authMethod transport.AuthMethod,
) *goGit.Repository {
	slog.InfoContext(ctx, "Detected remote repository is empty! Initializing repo locally")

	// Initialize repository locally.
	repo, err := goGit.PlainInitWithOptions(dirPath, &goGit.PlainInitOptions{
		InitOptions: goGit.InitOptions{
			DefaultBranch: plumbing.Main,
		},
	})
	assert.AssertErrNil(ctx, err, "Failed to initialize repo locally")

	// Add remote repository as 'origin'.
	_, err = repo.CreateRemote(&goGitConfig.RemoteConfig{
		Name: goGit.DefaultRemoteName,
		URLs: []string{originURL},
	})
	assert.AssertErrNil(ctx, err, "Failed adding 'origin' remote to the initilized local repo")

	// Create and push an empty commit.

	workTree, err := repo.Worktree()
	assert.AssertErrNil(ctx, err, "Failed getting repo worktree")

	_, err = workTree.Commit("chore : init", &goGit.CommitOptions{
		Author: &object.Signature{
			Name:  "KubeAid Bootstrap Script",
			Email: "info@obmondo.com",
			When:  time.Now(),
		},
		AllowEmptyCommits: true,
	})
	assert.AssertErrNil(ctx, err, "Failed creating init git commit")

	err = repo.Push(&goGit.PushOptions{
		RemoteName: goGit.DefaultRemoteName,
		Auth:       authMethod,
		CABundle:   config.ParsedGeneralConfig.Git.CABundle,
	})
	assert.AssertErrNil(ctx, err, "Failed pushing commit to upstream")

	return repo
}

// Returns whether the given git repository is private or not.
func isRepoPrivate(ctx context.Context, repoURL string) (bool, error) {
	// User is using SSH private key to authenticate against the git server.
	// The repository is then definitely private.
	if config.ParsedGeneralConfig.Git.SSHPrivateKeyConfig != nil {
		return true, nil
	}

	// We'll send an HTTP GET request to the repository URL.
	// If the request succeeds with a statuscode of 200, that means the repository is public.
	// Otherwise, it's private.

	client := &http.Client{}

	// Use CA bundle, provided by the user, when dealing with his / her git server.
	caBundle := config.ParsedGeneralConfig.Git.CABundle
	if (repoURL != constants.RepoURLObmondoKubeAid) && (len(caBundle) > 0) {
		certPool := x509.NewCertPool()

		ok := certPool.AppendCertsFromPEM(caBundle)
		assert.Assert(ctx, ok, "Failed to add custom CA bundle to cert pool")

		client.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs: certPool,
			},
		}
	}

	request, err := http.NewRequest(http.MethodGet, repoURL, nil)
	assert.AssertErrNil(ctx, err, "Failed constructing HTTP request")

	response, err := client.Do(request)
	if err != nil {
		return false, err
	}
	defer response.Body.Close()

	isRepoPrivate := (response.StatusCode != http.StatusOK)
	return isRepoPrivate, nil
}
