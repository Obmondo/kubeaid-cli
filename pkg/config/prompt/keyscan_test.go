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
		name         string
		rawURL       string
		wantHost     string
		wantPort     int
		wantExplicit bool
		wantErr      bool
		skip         bool
	}{
		{
			name:   "https url skipped",
			rawURL: "https://github.com/Obmondo/KubeAid.git",
			skip:   true,
		},
		{
			name:         "scp-style has no explicit port",
			rawURL:       "git@github.com:Obmondo/kubeaid-config.git",
			wantHost:     "github.com",
			wantPort:     22,
			wantExplicit: false,
		},
		{
			name:         "ssh url with explicit port",
			rawURL:       "ssh://git@gitea.example.com:2223/acme/kubeaid-config.git",
			wantHost:     "gitea.example.com",
			wantPort:     2223,
			wantExplicit: true,
		},
		{
			name:         "scp-style self-hosted forge stays port 22 until prompted",
			rawURL:       "git@gitea.example.com:acme/kubeaid-config.git",
			wantHost:     "gitea.example.com",
			wantPort:     22,
			wantExplicit: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			host, port, explicit, err := hostPortFromGitURL(tc.rawURL)
			if tc.skip {
				require.NoError(t, err)
				assert.Empty(t, host)
				assert.Zero(t, port)
				assert.False(t, explicit)
				return
			}
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantHost, host)
			assert.Equal(t, tc.wantPort, port)
			assert.Equal(t, tc.wantExplicit, explicit)
		})
	}
}

func TestFormatKnownHostsLine(t *testing.T) {
	t.Parallel()

	keyLine := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIHZLLpBn+ig1bdyf+9SLB0wbIMcfaNs+M+Co7ZW0ykzl"
	pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(keyLine))
	require.NoError(t, err)

	got := formatKnownHostsLine("gitea.example.com", 2223, pub)
	assert.Equal(t, "[gitea.example.com]:2223 "+keyLine, got)

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
			line:     "gitea.example.com ecdsa-sha2-nistp256 AAAA",
			wantHost: "gitea.example.com",
			wantPort: 22,
			wantOK:   true,
		},
		{
			name:     "bracketed host with port",
			line:     "[gitea.example.com]:2223 ecdsa-sha2-nistp256 AAAA",
			wantHost: "gitea.example.com",
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

func TestPopulateGitKnownHostsAsksPortForScpStyleAndDedupes(t *testing.T) {
	// Not parallel: mutates package-level ask/scan funcs.
	stale := "gitea.example.com ecdsa-sha2-nistp256 STALE"
	fresh := "[gitea.example.com]:2223 ecdsa-sha2-nistp256 FRESH"

	cfg := &PromptedConfig{
		KubeaidForkURL:       "https://github.com/Obmondo/KubeAid.git",
		KubeaidConfigForkURL: "git@gitea.example.com:acme/kubeaid-config.git",
		GitKnownHosts:        []string{stale, stale},
	}

	origScan := scanSSHHostKeyFunc
	origAsk := askSSHPortFunc
	t.Cleanup(func() {
		scanSSHHostKeyFunc = origScan
		askSSHPortFunc = origAsk
	})

	asked := false
	askSSHPortFunc = func(host string, defaultPort int) (int, error) {
		asked = true
		require.Equal(t, "gitea.example.com", host)
		require.Equal(t, 22, defaultPort)
		return 2223, nil
	}
	scanSSHHostKeyFunc = func(host string, port int) (string, error) {
		require.Equal(t, "gitea.example.com", host)
		require.Equal(t, 2223, port)
		return fresh, nil
	}

	populateGitKnownHosts(cfg)
	require.True(t, asked)
	require.Len(t, cfg.GitKnownHosts, 1)
	assert.Equal(t, fresh, cfg.GitKnownHosts[0])
}

func TestPopulateGitKnownHostsUsesExplicitSSHURLPort(t *testing.T) {
	// Not parallel: mutates package-level ask/scan funcs.
	fresh := "[forge.example.com]:2223 ecdsa-sha2-nistp256 FRESH"
	cfg := &PromptedConfig{
		KubeaidForkURL:       "https://github.com/Obmondo/KubeAid.git",
		KubeaidConfigForkURL: "ssh://git@forge.example.com:2223/acme/kubeaid-config.git",
	}

	origScan := scanSSHHostKeyFunc
	origAsk := askSSHPortFunc
	t.Cleanup(func() {
		scanSSHHostKeyFunc = origScan
		askSSHPortFunc = origAsk
	})

	askSSHPortFunc = func(string, int) (int, error) {
		t.Fatal("should not prompt when URL encodes an explicit port")
		return 0, nil
	}
	scanSSHHostKeyFunc = func(host string, port int) (string, error) {
		require.Equal(t, "forge.example.com", host)
		require.Equal(t, 2223, port)
		return fresh, nil
	}

	populateGitKnownHosts(cfg)
	require.Equal(t, []string{fresh}, cfg.GitKnownHosts)
}

func TestPopulateGitKnownHostsPrefillsExistingKnownHostsPort(t *testing.T) {
	// Not parallel: mutates package-level ask/scan funcs.
	existing := "[forge.example.com]:2223 ecdsa-sha2-nistp256 EXISTING"
	cfg := &PromptedConfig{
		KubeaidForkURL:       "https://github.com/Obmondo/KubeAid.git",
		KubeaidConfigForkURL: "git@forge.example.com:acme/kubeaid-config.git",
		GitKnownHosts:        []string{existing},
	}

	origScan := scanSSHHostKeyFunc
	origAsk := askSSHPortFunc
	t.Cleanup(func() {
		scanSSHHostKeyFunc = origScan
		askSSHPortFunc = origAsk
	})

	askSSHPortFunc = func(host string, defaultPort int) (int, error) {
		require.Equal(t, "forge.example.com", host)
		require.Equal(t, 2223, defaultPort)
		return defaultPort, nil
	}
	scanSSHHostKeyFunc = func(string, int) (string, error) {
		t.Fatal("should skip scan when host:port already present")
		return "", nil
	}

	populateGitKnownHosts(cfg)
	require.Equal(t, []string{existing}, cfg.GitKnownHosts)
}

func TestValidateSSHPort(t *testing.T) {
	t.Parallel()

	assert.NoError(t, validateSSHPort("22"))
	assert.NoError(t, validateSSHPort("2223"))
	assert.Error(t, validateSSHPort(""))
	assert.Error(t, validateSSHPort("0"))
	assert.Error(t, validateSSHPort("70000"))
	assert.Error(t, validateSSHPort("abc"))
}
