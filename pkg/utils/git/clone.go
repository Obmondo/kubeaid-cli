package git

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"log/slog"
	"net/http"
	"os"

	goGit "github.com/go-git/go-git/v5"
	goGitConfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
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

	// Use some authentication method, if the repository visibility is private.
	isPrivate, err := IsRepoPrivate(ctx, url)
	assert.AssertErrNil(ctx, err, "Failed to determine repo visibility")
	if isPrivate {
		opts.Auth = authMethod
	}

	repo, err := goGit.PlainClone(dirPath, false, opts)
	if errors.Is(err, transport.ErrEmptyRemoteRepository) {
		// Remote repository is empty,
		// which means we need to initialize a local repository and add the remote repository as
		// 'origin'.
		repo = initRepo(ctx, dirPath, config.ParsedGeneralConfig.Forks.KubeaidConfigForkURL)
	} else {
		assert.AssertErrNil(ctx, err, "Failed cloning repo")
	}
	return repo
}

// Initializes a repository locally, at the given path.
// Then adds the given remote repository as 'origin'.
func initRepo(ctx context.Context, dirPath, originURL string) *goGit.Repository {
	slog.InfoContext(ctx, "Detected remote repository is empty! Initializing repo locally")

	repo, err := goGit.PlainInitWithOptions(dirPath, &goGit.PlainInitOptions{
		InitOptions: goGit.InitOptions{
			DefaultBranch: plumbing.Main,
		},
	})
	assert.AssertErrNil(ctx, err, "Failed to initialize repo locally")

	_, err = repo.CreateRemote(&goGitConfig.RemoteConfig{
		Name: goGit.DefaultRemoteName,
		URLs: []string{originURL},
	})
	assert.AssertErrNil(ctx, err, "Failed adding 'origin' remote to the initilized local repo")

	return repo
}

// IsRepoPrivate checks if the repository is private using the appropriate API
func IsRepoPrivate(ctx context.Context, repoURL string) (bool, error) {
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
		return false, nil
	}

	// Request was successful, which means the repo is public.
	return true, nil
}
