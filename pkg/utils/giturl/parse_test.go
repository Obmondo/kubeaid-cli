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
			name:      "ssh:// self-hosted forge with custom port",
			url:       "ssh://git@git.example.com:2223/acme/kubeaid-config-acme.git",
			wantHost:  "git.example.com:2223",
			wantOwner: "acme",
			wantRepo:  "kubeaid-config-acme",
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

	pCustom := &ParsedURL{Host: "git.example.com:2223", Owner: "acme", Repo: "kubeaid-config-acme"}
	assert.Equal(t, "https://git.example.com:2223/acme/kubeaid-config-acme.git", pCustom.HTTPCloneURL())
}

func TestHostName(t *testing.T) {
	tests := []struct {
		name string
		host string
		want string
	}{
		{name: "bare hostname", host: "github.com", want: "github.com"},
		{name: "host with port", host: "git.example.com:2223", want: "git.example.com"},
		{name: "ipv4 host", host: "192.168.1.10", want: "192.168.1.10"},
		{name: "ipv4 host with port", host: "192.168.1.10:22", want: "192.168.1.10"},
		{name: "empty host", host: "", want: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := &ParsedURL{Host: tc.host}
			assert.Equal(t, tc.want, p.HostName())
		})
	}
}
