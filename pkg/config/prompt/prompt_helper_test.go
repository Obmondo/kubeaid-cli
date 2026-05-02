// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package prompt

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

func TestWrapLine(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		maxWidth int
		want     []string
	}{
		{
			name:     "line short enough is returned as-is",
			line:     "Region: eu-west-1",
			maxWidth: 80,
			want:     []string{"Region: eu-west-1"},
		},
		{
			name:     "wraps a long value at the indent point",
			line:     "Region: " + strings.Repeat("a", 30),
			maxWidth: 20,
			want: []string{
				"Region: " + strings.Repeat("a", 12),
				"        " + strings.Repeat("a", 12),
				"        " + strings.Repeat("a", 6),
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, wrapLine(tc.line, tc.maxWidth))
		})
	}
}

func TestExpandTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "tilde-slash expands to home",
			in:   "~/.ssh/id_ed25519",
			want: home + "/.ssh/id_ed25519",
		},
		{
			name: "absolute path is returned unchanged",
			in:   "/etc/passwd",
			want: "/etc/passwd",
		},
		{
			name: "bare tilde without slash is not expanded",
			in:   "~",
			want: "~",
		},
		{
			name: "tilde-username form is not expanded",
			in:   "~root/file",
			want: "~root/file",
		},
		{
			name: "empty string is returned unchanged",
			in:   "",
			want: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, expandTilde(tc.in))
		})
	}
}

func TestExpandPaths(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	tests := []struct {
		name string
		cfg  *PromptedConfig
		want *PromptedConfig
	}{
		{
			name: "tilde paths expanded; absolute path untouched",
			cfg: &PromptedConfig{
				SSHKeyPath:                 "~/.ssh/id_ed25519",
				KubeaidConfigDeployKeyPath: "~/.ssh/deploy",
				HetznerSSHKeyPath:          "/absolute/key",
			},
			want: &PromptedConfig{
				SSHKeyPath:                 home + "/.ssh/id_ed25519",
				KubeaidConfigDeployKeyPath: home + "/.ssh/deploy",
				HetznerSSHKeyPath:          "/absolute/key",
			},
		},
		{
			name: "all empty paths stay empty",
			cfg:  &PromptedConfig{},
			want: &PromptedConfig{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expandPaths(tc.cfg)
			assert.Equal(t, tc.want.SSHKeyPath, tc.cfg.SSHKeyPath)
			assert.Equal(t, tc.want.KubeaidConfigDeployKeyPath, tc.cfg.KubeaidConfigDeployKeyPath)
			assert.Equal(t, tc.want.HetznerSSHKeyPath, tc.cfg.HetznerSSHKeyPath)
		})
	}
}

func TestValidateSSHPrivateKey(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	pemBlock, err := ssh.MarshalPrivateKey(priv, "")
	require.NoError(t, err)
	validKey := pem.EncodeToMemory(pemBlock)

	tests := []struct {
		name    string
		data    []byte
		wantErr bool
	}{
		{name: "valid ed25519 PEM passes", data: validKey},
		{name: "garbage bytes fail", data: []byte("not a key"), wantErr: true},
		{name: "empty bytes fail", data: nil, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateSSHPrivateKey(tc.data)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestValidateSSHKeyPath(t *testing.T) {
	dir := t.TempDir()

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	pemBlock, err := ssh.MarshalPrivateKey(priv, "")
	require.NoError(t, err)
	validKeyPath := filepath.Join(dir, "id_ed25519")
	require.NoError(t, os.WriteFile(validKeyPath, pem.EncodeToMemory(pemBlock), 0o600))

	junkPath := filepath.Join(dir, "junk")
	require.NoError(t, os.WriteFile(junkPath, []byte("not a key"), 0o600))

	tests := []struct {
		name      string
		path      string
		wantErr   bool
		errSubstr string
	}{
		{name: "valid PEM passes", path: validKeyPath},
		{name: "empty path is required", path: "", wantErr: true, errSubstr: "required"},
		{
			name:      "missing file fails with file-not-found",
			path:      filepath.Join(dir, "nope"),
			wantErr:   true,
			errSubstr: "file not found",
		},
		{
			name:      "non-key contents fail",
			path:      junkPath,
			wantErr:   true,
			errSubstr: "not a valid SSH private key",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateSSHKeyPath(tc.path)
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errSubstr)
				return
			}
			assert.NoError(t, err)
		})
	}
}
