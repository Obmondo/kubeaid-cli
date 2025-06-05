package git

import (
	"context"
	"log/slog"
	"net/url"
	"strings"

	goGit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
)

func GetDefaultBranchName(ctx context.Context,
	authMethod transport.AuthMethod,
	repo *goGit.Repository,
) string {
	remote, err := repo.Remote(goGit.DefaultRemoteName)
	assert.AssertErrNil(ctx, err, "Failed getting repo 'origin' remote")

	refs, err := remote.List(&goGit.ListOptions{
		Auth:     authMethod,
		CABundle: config.ParsedGeneralConfig.Git.CABundle,
	})
	assert.AssertErrNil(ctx, err, "Failed listing refs for 'origin' remote")

	for _, ref := range refs {
		if ref.Name() == plumbing.HEAD {
			target := ref.Target().String()

			defaultBranchName := target[11:] // Remove "refs/heads/" prefix.
			slog.InfoContext(ctx,
				"Detected default branch name",
				slog.String("branch", defaultBranchName),
			)

			return defaultBranchName
		}
	}

	panic("Failed detecting default branch name")
}

// Returns hostname of customer's git server.
func GetCustomerGitServerHostName(ctx context.Context) string {
	kubeaidConfigURL, err := url.Parse(config.ParsedGeneralConfig.Forks.KubeaidConfigForkURL)
	assert.AssertErrNil(ctx, err, "Failed parsing KubeAid config URL")

	return kubeaidConfigURL.Hostname()
}

// Return host and port of a git url
func parseGitURL(ctx context.Context, gitURL string) (string, string) {
	var parsedURL *url.URL
	var err error

	// Convert SSH URL to a format that can be parsed
	if strings.HasPrefix(gitURL, "git@") {
		// Convert to HTTPS format for parsing
		parts := strings.Split(gitURL, ":")
		if len(parts) == 2 {
			gitURL = "https://" + parts[0][4:] + "/" + parts[1]
		}
	}

	// Parse the URL
	parsedURL, err = url.Parse(gitURL)
	if err != nil {
		assert.AssertErrNil(ctx, err, "Error parsing Git URL")
		return "", ""
	}

	domain := parsedURL.Hostname()
	port := parsedURL.Port()

	if port == "" {
		// Set default port based on the scheme
		if parsedURL.Scheme == "https" {
			port = "443"
		} else if parsedURL.Scheme == "http" {
			port = "80"
		} else {
			port = "22"
		}
	}

	return domain, port
}
