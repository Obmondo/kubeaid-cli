// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package url

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name           string
		url            string
		wantErr        bool
		wantProtocol   Protocol
		wantHost       string
		wantOwner      string
		wantRepository string
	}{
		{
			name:           "https github with .git",
			url:            "https://github.com/Obmondo/kubeaid-config.git",
			wantProtocol:   ProtocolHTTPs,
			wantHost:       "github.com",
			wantOwner:      "Obmondo",
			wantRepository: "kubeaid-config",
		},
		{
			name:           "https github without .git",
			url:            "https://github.com/foo/bar",
			wantProtocol:   ProtocolHTTPs,
			wantHost:       "github.com",
			wantOwner:      "foo",
			wantRepository: "bar",
		},
		{
			name:           "http self-hosted forge",
			url:            "http://gitea.local:3000/org/repo.git",
			wantProtocol:   ProtocolHTTP,
			wantHost:       "gitea.local:3000",
			wantOwner:      "org",
			wantRepository: "repo",
		},
		{
			name:           "scp-style github",
			url:            "git@github.com:Obmondo/kubeaid-config.git",
			wantProtocol:   ProtocolSCP,
			wantHost:       "github.com",
			wantOwner:      "Obmondo",
			wantRepository: "kubeaid-config",
		},
		{
			name:           "scp:// schemed URL",
			url:            "scp://git@github.com:Obmondo/kubeaid-config.git",
			wantProtocol:   ProtocolSCP,
			wantHost:       "github.com",
			wantOwner:      "Obmondo",
			wantRepository: "kubeaid-config",
		},
		{
			// The port must survive into Host — HostName strips it for
			// consumers that can't take one.
			name:           "ssh:// self-hosted forge with custom port",
			url:            "ssh://git@git.example.com:2223/acme/kubeaid-config-acme.git",
			wantProtocol:   ProtocolSSH,
			wantHost:       "git.example.com:2223",
			wantOwner:      "acme",
			wantRepository: "kubeaid-config-acme",
		},
		{
			name:           "https self-hosted with port",
			url:            "https://gitea.example.com:8443/org/repo.git",
			wantProtocol:   ProtocolHTTPs,
			wantHost:       "gitea.example.com:8443",
			wantOwner:      "org",
			wantRepository: "repo",
		},
		{
			// Gitea / GitLab nested groups are out of scope : we take
			// the leading two segments and ignore the rest.
			name:           "nested path keeps first two segments",
			url:            "https://gitlab.com/group/subgroup/repo.git",
			wantProtocol:   ProtocolHTTPs,
			wantHost:       "gitlab.com",
			wantOwner:      "group",
			wantRepository: "subgroup",
		},
		{
			name:           "surrounding whitespace trimmed",
			url:            "  https://github.com/Obmondo/kubeaid-config.git  ",
			wantProtocol:   ProtocolHTTPs,
			wantHost:       "github.com",
			wantOwner:      "Obmondo",
			wantRepository: "kubeaid-config",
		},
		{name: "empty input rejected", url: "", wantErr: true},
		{name: "whitespace-only rejected", url: "   ", wantErr: true},
		{name: "missing repository segment rejected", url: "https://github.com/justowner", wantErr: true},
		{name: "missing owner segment rejected", url: "https://github.com//repo.git", wantErr: true},
		// originShortName in pkg/utils/git relies on Parse returning an
		// error (rather than panicking) for malformed remote URLs, so
		// that it can fall back to the generic "repo" label.
		{name: "malformed URL without separator rejected", url: "not a git URL", wantErr: true},
		{name: "SCP URL with empty path rejected", url: "git@github.com:", wantErr: true},
		{name: "SCP URL with multiple @ rejected", url: "git@foo@github.com:org/repo.git", wantErr: true},
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

			assert.Equal(t, tc.wantProtocol, parsed.Protocol)
			assert.Equal(t, tc.wantHost, parsed.Host)
			assert.Equal(t, tc.wantOwner, parsed.Owner)
			assert.Equal(t, tc.wantRepository, parsed.Repository)
		})
	}
}

func TestAsHTTPsURL(t *testing.T) {
	tests := []struct {
		name   string
		parsed *Parsed
		want   string
	}{
		{
			name: "https URL",
			parsed: &Parsed{
				Protocol: ProtocolHTTPs, Host: "github.com",
				Owner: "Obmondo", Repository: "kubeaid-config",
			},
			want: "https://github.com/Obmondo/kubeaid-config.git",
		},
		{
			// A port on an HTTP based URL is an HTTP(s) port, so it's
			// legitimate and must be preserved.
			name: "http URL with port keeps the port",
			parsed: &Parsed{
				Protocol: ProtocolHTTP, Host: "forge.example.com:8443",
				Owner: "team", Repository: "repo",
			},
			want: "https://forge.example.com:8443/team/repo.git",
		},
		{
			// A port on an SSH based URL is the SSH port, which is
			// meaningless to HTTPs : keeping it produces a clickable URL
			// which points the browser at the SSH daemon and 404s.
			name: "ssh URL with port drops the port",
			parsed: &Parsed{
				Protocol: ProtocolSSH, Host: "git.example.com:2223",
				Owner: "acme", Repository: "kubeaid-config-acme",
			},
			want: "https://git.example.com/acme/kubeaid-config-acme.git",
		},
		{
			name: "scp URL",
			parsed: &Parsed{
				Protocol: ProtocolSCP, Host: "github.com",
				Owner: "Obmondo", Repository: "kubeaid-config",
			},
			want: "https://github.com/Obmondo/kubeaid-config.git",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.parsed.AsHTTPsURL())
		})
	}
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
			parsed := &Parsed{Host: tc.host}
			assert.Equal(t, tc.want, parsed.HostName())
		})
	}
}
