// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package giturl

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		wantErr   bool
		wantHost  string
		wantOwner string
		wantRepo  string
	}{
		{
			name:      "https github with .git",
			url:       "https://github.com/Obmondo/kubeaid-config.git",
			wantHost:  "github.com",
			wantOwner: "Obmondo",
			wantRepo:  "kubeaid-config",
		},
		{
			name:      "https github without .git",
			url:       "https://github.com/foo/bar",
			wantHost:  "github.com",
			wantOwner: "foo",
			wantRepo:  "bar",
		},
		{
			name:      "scp-style github",
			url:       "git@github.com:Obmondo/kubeaid-config.git",
			wantHost:  "github.com",
			wantOwner: "Obmondo",
			wantRepo:  "kubeaid-config",
		},
		{
			name:      "ssh:// self-hosted gitea with custom port",
			url:       "ssh://git@gitea.obmondo.com:2223/EnableIT/kubeaid-config-enableit.git",
			wantHost:  "gitea.obmondo.com:2223",
			wantOwner: "EnableIT",
			wantRepo:  "kubeaid-config-enableit",
		},
		{
			name:      "https self-hosted with port",
			url:       "https://gitea.example.com:8443/org/repo.git",
			wantHost:  "gitea.example.com:8443",
			wantOwner: "org",
			wantRepo:  "repo",
		},
		{
			name:      "nested path keeps first two segments",
			url:       "https://gitlab.com/group/subgroup/repo.git",
			wantHost:  "gitlab.com",
			wantOwner: "group",
			wantRepo:  "subgroup",
		},
		{name: "empty input rejected", url: "", wantErr: true},
		{name: "whitespace-only rejected", url: "   ", wantErr: true},
		{name: "missing repo segment rejected", url: "https://github.com/justowner", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := Parse(tc.url)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, parsed)
			assert.Equal(t, tc.wantHost, parsed.Host)
			assert.Equal(t, tc.wantOwner, parsed.Owner)
			assert.Equal(t, tc.wantRepo, parsed.Repo)
		})
	}
}

func TestHTTPCloneURL(t *testing.T) {
	p := &ParsedURL{Host: "github.com", Owner: "Obmondo", Repo: "kubeaid-config"}
	assert.Equal(t, "https://github.com/Obmondo/kubeaid-config.git", p.HTTPCloneURL())

	pCustom := &ParsedURL{Host: "gitea.obmondo.com:2223", Owner: "EnableIT", Repo: "kubeaid-config-enableit"}
	assert.Equal(t, "https://gitea.obmondo.com:2223/EnableIT/kubeaid-config-enableit.git", pCustom.HTTPCloneURL())
}
