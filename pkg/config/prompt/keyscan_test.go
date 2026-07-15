// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package prompt

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

func TestHostPortFromGitURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		rawURL   string
		wantHost string
		wantPort int
		wantErr  bool
		skip     bool
	}{
		{
			name:   "https url skipped",
			rawURL: "https://github.com/Obmondo/KubeAid.git",
			skip:   true,
		},
		{
			name:     "scp-style github defaults port 22",
			rawURL:   "git@github.com:Obmondo/kubeaid-config.git",
			wantHost: "github.com",
			wantPort: 22,
		},
		{
			name:     "ssh url with explicit port",
			rawURL:   "ssh://git@gitea.example.com:2223/acme/kubeaid-config.git",
			wantHost: "gitea.example.com",
			wantPort: 2223,
		},
		{
			name:     "scp-style gitea.obmondo.com uses default ssh port override",
			rawURL:   "git@gitea.obmondo.com:Obmondo/kubeaid-config.git",
			wantHost: "gitea.obmondo.com",
			wantPort: 2223,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			host, port, err := hostPortFromGitURL(tc.rawURL)
			if tc.skip {
				require.NoError(t, err)
				assert.Empty(t, host)
				assert.Zero(t, port)
				return
			}
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantHost, host)
			assert.Equal(t, tc.wantPort, port)
		})
	}
}

func TestFormatKnownHostsLine(t *testing.T) {
	t.Parallel()

	keyLine := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIHZLLpBn+ig1bdyf+9SLB0wbIMcfaNs+M+Co7ZW0ykzl"
	pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(keyLine))
	require.NoError(t, err)

	got := formatKnownHostsLine("gitea.obmondo.com", 2223, pub)
	assert.Equal(t, "[gitea.obmondo.com]:2223 "+keyLine, got)

	got = formatKnownHostsLine("github.com", 22, pub)
	assert.Equal(t, "github.com "+keyLine, got)
}

func TestHostPortFromKnownHostsLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		line     string
		wantHost string
		wantPort int
		wantOK   bool
	}{
		{
			name:     "bare hostname",
			line:     "gitea.obmondo.com ecdsa-sha2-nistp256 AAAA",
			wantHost: "gitea.obmondo.com",
			wantPort: 22,
			wantOK:   true,
		},
		{
			name:     "bracketed host with port",
			line:     "[gitea.obmondo.com]:2223 ecdsa-sha2-nistp256 AAAA",
			wantHost: "gitea.obmondo.com",
			wantPort: 2223,
			wantOK:   true,
		},
		{
			name:   "invalid",
			line:   "not-a-known-hosts-line",
			wantOK: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			host, port, ok := hostPortFromKnownHostsLine(tc.line)
			assert.Equal(t, tc.wantOK, ok)
			if !tc.wantOK {
				return
			}
			assert.Equal(t, tc.wantHost, host)
			assert.Equal(t, tc.wantPort, port)
		})
	}
}

func TestPopulateGitKnownHostsReplacesStaleHostAndDedupes(t *testing.T) {
	t.Parallel()

	stale := "gitea.obmondo.com ecdsa-sha2-nistp256 STALE"
	fresh := "[gitea.obmondo.com]:2223 ecdsa-sha2-nistp256 FRESH"

	cfg := &PromptedConfig{
		KubeaidForkURL:       "https://github.com/Obmondo/KubeAid.git",
		KubeaidConfigForkURL: "git@gitea.obmondo.com:Obmondo/kubeaid-config.git",
		GitKnownHosts:        []string{stale, stale},
	}

	origScan := scanSSHHostKeyFunc
	t.Cleanup(func() { scanSSHHostKeyFunc = origScan })
	scanSSHHostKeyFunc = func(host string, port int) (string, error) {
		require.Equal(t, "gitea.obmondo.com", host)
		require.Equal(t, 2223, port)
		return fresh, nil
	}

	populateGitKnownHosts(cfg)
	require.Len(t, cfg.GitKnownHosts, 1)
	assert.Equal(t, fresh, cfg.GitKnownHosts[0])
}
