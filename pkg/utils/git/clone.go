package git

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strings"
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

// Clones the given git repository into the given directory (only if the repo doesn't already exist
// in there).
// If the repo already exists, then it checks out to the default branch and fetches the latest
// changes.
func CloneRepo(ctx context.Context,
	url,
	dirPath string,
	authMethod transport.AuthMethod,
) *goGit.Repository {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("repo", url), slog.String("dir", dirPath),
	})

	// If the repo is already cloned.
	if _, err := os.ReadDir(dirPath); err == nil {
		repo, err := goGit.PlainOpen(dirPath)
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

	repo, err := goGit.PlainClone(dirPath, false, opts)
	if errors.Is(err, transport.ErrEmptyRemoteRepository) &&
		(url == config.ParsedGeneralConfig.Forks.KubeaidConfigForkURL) {
		// Remote KubeAid Config repository is empty.
		// So, we need to initialize the repository locally,
		// add the remote repository as 'origin',
		// and create and push an empty commit.
		repo = initRepo(ctx,
			dirPath,
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

// IsRepoPrivate checks if the repository is private using the appropriate API
func isRepoPrivate(ctx context.Context, repoURL string) (bool, error) {
	urlType := determineURLType(repoURL)

	// SSH git repo are private
	if (len(config.ParsedSecretsConfig.Git.SSHPrivateKey) > 0) && (urlType == "SSH") {
		return true, nil
	}

	// Create a new HTTP client
	client := &http.Client{}

	// Use CA bundle, provided by the user, when dealing with user's git server.
	caBundle := config.ParsedGeneralConfig.Git.CABundle
	if (repoURL != constants.RepoURLObmondoKubeAid) && (len(caBundle) > 0) {
		certPool := x509.NewCertPool()

		ok := certPool.AppendCertsFromPEM(caBundle)
		if !ok {
			slog.ErrorContext(ctx, "Failed to add custom CA bundle to cert pool")
			os.Exit(1)
		}

		client.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs: certPool,
			},
		}
	}

	// Construct a new request.
	request, err := http.NewRequest(http.MethodGet, repoURL, nil)
	assert.AssertErrNil(ctx, err, "Failed constructing HTTP request")

	// Make the request
	response, err := client.Do(request)
	if err != nil {
		return false, err
	}
	defer response.Body.Close()

	// If the request was unsuccessful, then the repo isn't public.
	if response.StatusCode != http.StatusOK {
		// If status is NOT 200 OK, it means it's likely private.
		return true, nil
	}

	// Request was successful (status was 200 OK), which means the repo is public.
	return false, nil
}

func determineURLType(repoURL string) string {
	// Check the URL scheme
	if strings.HasPrefix(repoURL, "ssh://") {
		return "SSH"
	} else if strings.HasPrefix(repoURL, "https://") {
		return "HTTPS"
	} else if strings.Contains(repoURL, "@") && strings.Contains(repoURL, ":") {
		return "SSH"
	}

	return "Unknown"
}
