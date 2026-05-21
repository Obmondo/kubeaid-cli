// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package git

import (
	"context"
	"path"
	"testing"

	goGit "github.com/go-git/go-git/v5"
	goGitConfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/giturl"
)

func TestGetRepoDir(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		wantHost  string
		wantOwner string
		wantRepo  string
	}{
		{
			name:      "github HTTPS URL with .git suffix",
			url:       "https://github.com/Obmondo/kubeaid-cli.git",
			wantHost:  "github.com",
			wantOwner: "Obmondo",
			wantRepo:  "kubeaid-cli",
		},
		{
			name:      "github SCP-style SSH URL",
			url:       "git@github.com:Obmondo/kubeaid-config.git",
			wantHost:  "github.com",
			wantOwner: "Obmondo",
			wantRepo:  "kubeaid-config",
		},
		{
			name:      "github HTTPS without .git suffix",
			url:       "https://github.com/foo/bar",
			wantHost:  "github.com",
			wantOwner: "foo",
			wantRepo:  "bar",
		},
		{
			// Custom SSH port — must end up in the on-disk path
			// without the colon, since docker -v <src>:<dst> would
			// otherwise read the port as a third path component.
			name:      "self-hosted forge on custom SSH port",
			url:       "ssh://git@git.example.com:2223/acme/kubeaid-config-acme.git",
			wantHost:  "git.example.com",
			wantOwner: "acme",
			wantRepo:  "kubeaid-config-acme",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := giturl.Parse(tc.url)
			require.NoError(t, err)

			got := GetRepoDir(parsed)
			want := path.Join(constants.TempDirectory, tc.wantHost, tc.wantOwner, tc.wantRepo)
			assert.Equal(t, want, got)
		})
	}
}

func TestOriginShortName(t *testing.T) {
	tests := []struct {
		name      string
		originURL string
		want      string
	}{
		{
			name: "missing origin remote falls back",
			want: "repo",
		},
		{
			name:      "malformed origin URL falls back",
			originURL: "not a git URL",
			want:      "repo",
		},
		{
			name:      "SCP-style SSH origin",
			originURL: "git@github.com:Obmondo/kubeaid-config.git",
			want:      "Obmondo/kubeaid-config",
		},
		{
			name:      "HTTPS origin",
			originURL: "https://github.com/Obmondo/kubeaid-cli.git",
			want:      "Obmondo/kubeaid-cli",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := newTestRepo(t)
			if tc.originURL != "" {
				createOriginRemote(t, repo, tc.originURL)
			}

			assert.Equal(t, tc.want, originShortName(repo))
		})
	}
}

func TestBuildPRCompareURL(t *testing.T) {
	tests := []struct {
		name      string
		originURL string
		want      string
	}{
		{
			name:      "HTTPS origin trims git suffix",
			originURL: "https://github.com/Obmondo/kubeaid-config.git",
			want:      "https://github.com/Obmondo/kubeaid-config/compare/main...feature/test",
		},
		{
			name:      "SCP-style SSH origin becomes HTTPS compare URL",
			originURL: "git@github.com:Obmondo/kubeaid-config.git",
			want:      "https://github.com/Obmondo/kubeaid-config/compare/main...feature/test",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := newTestRepo(t)
			createOriginRemote(t, repo, tc.originURL)

			got := BuildPRCompareURL(repo, "main", "feature/test")
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestBuildPRCompareURLPanicsWhenOriginIsMissing(t *testing.T) {
	repo := newTestRepo(t)

	assert.PanicsWithValue(t,
		"BuildPRCompareURL: get origin remote: remote not found",
		func() {
			BuildPRCompareURL(repo, "main", "feature/test")
		},
	)
}

func TestParseURL(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		wantHost  string
		wantOwner string
		wantRepo  string
		wantErr   assert.ErrorAssertionFunc
	}{
		{
			name:      "valid GitHub URL",
			url:       "https://github.com/Obmondo/kubeaid-config.git",
			wantHost:  "github.com",
			wantOwner: "Obmondo",
			wantRepo:  "kubeaid-config",
			wantErr:   assert.NoError,
		},
		{
			name:    "invalid URL",
			url:     "not a git URL",
			wantErr: assert.Error,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := ParseURL(tc.url)
			tc.wantErr(t, err)
			if err != nil {
				return
			}

			assert.Equal(t, tc.wantHost, parsed.HostName())
			assert.Equal(t, tc.wantOwner, parsed.Owner)
			assert.Equal(t, tc.wantRepo, parsed.Repo)
		})
	}
}

func TestRemoteHEADRefCache(t *testing.T) {
	repo := newTestRepo(t)

	SetRemoteHEADRef(context.Background(), repo, "main")

	ref, err := repo.Reference(plumbing.ReferenceName(remoteHEADRefName), false)
	require.NoError(t, err)
	assert.Equal(t, remoteBranchPrefix+"main", ref.Target().String())
	assert.Equal(t, "main", GetDefaultBranchName(context.Background(), nil, repo))
}

func newTestRepo(t *testing.T) *goGit.Repository {
	t.Helper()

	repo, err := goGit.PlainInit(t.TempDir(), false)
	require.NoError(t, err)
	return repo
}

func createOriginRemote(t *testing.T, repo *goGit.Repository, originURL string) {
	t.Helper()

	_, err := repo.CreateRemote(&goGitConfig.RemoteConfig{
		Name: goGit.DefaultRemoteName,
		URLs: []string{originURL},
	})
	require.NoError(t, err)
}
