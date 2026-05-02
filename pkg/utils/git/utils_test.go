// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package git

import (
	"path"
	"testing"

	gogiturl "github.com/kubescape/go-git-url"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
)

func TestIsHTTPURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{name: "https URL", url: "https://github.com/org/repo.git", want: true},
		{name: "http URL with port", url: "http://gitea.local:3000/org/repo.git", want: true},
		{name: "ssh URL", url: "ssh://git@github.com/org/repo.git", want: false},
		{name: "scp-style git URL", url: "git@github.com:org/repo.git", want: false},
		{name: "empty string", url: "", want: false},
		{name: "uppercase HTTPS not matched", url: "HTTPS://github.com/org/repo.git", want: false},
		{name: "leading whitespace not stripped", url: " https://github.com/org/repo.git", want: false},
		{name: "scheme-like prefix without colon-slashes", url: "httpsfoo://example", want: false},
		{name: "http inside path is not a prefix match", url: "git+http://example", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isHTTPURL(tc.url))
		})
	}
}

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
			url:       "https://github.com/Obmondo/kubeaid-bootstrap-script.git",
			wantHost:  "github.com",
			wantOwner: "Obmondo",
			wantRepo:  "kubeaid-bootstrap-script",
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
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := gogiturl.NewGitURL(tc.url)
			require.NoError(t, err)

			got := GetRepoDir(parsed)
			want := path.Join(constants.TempDirectory, tc.wantHost, tc.wantOwner, tc.wantRepo)
			assert.Equal(t, want, got)
		})
	}
}

func TestParseURL(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		wantErr   bool
		wantHost  string
		wantOwner string
		wantRepo  string
	}{
		{
			name:      "https URL parses host/owner/repo",
			url:       "https://github.com/Obmondo/kubeaid.git",
			wantHost:  "github.com",
			wantOwner: "Obmondo",
			wantRepo:  "kubeaid",
		},
		{
			name:      "scp-style github URL parses host/owner/repo",
			url:       "git@github.com:Obmondo/kubeaid-config.git",
			wantHost:  "github.com",
			wantOwner: "Obmondo",
			wantRepo:  "kubeaid-config",
		},
		{
			name:    "unsupported host returns error",
			url:     "https://example.invalid/o/r.git",
			wantErr: true,
		},
		{
			name:    "empty URL returns error",
			url:     "",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := ParseURL(tc.url)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, parsed)
			assert.Equal(t, tc.wantHost, parsed.GetHostName())
			assert.Equal(t, tc.wantOwner, parsed.GetOwnerName())
			assert.Equal(t, tc.wantRepo, parsed.GetRepoName())
		})
	}
}
