// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package prompt

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

// writeSSHKey puts a usable private key on disk and returns its path.
func writeSSHKey(t *testing.T, name string) string {
	t.Helper()

	_, private, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	pemBlock, err := ssh.MarshalPrivateKey(private, "")
	require.NoError(t, err)

	keyPath := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(keyPath, pem.EncodeToMemory(pemBlock), 0o600))

	return keyPath
}

func TestConfiguredSSHKeyPath(t *testing.T) {
	keyPath := writeSSHKey(t, "id_ed25519")
	missingPath := filepath.Join(t.TempDir(), "gone")

	notAKey := filepath.Join(t.TempDir(), "notakey")
	require.NoError(t, os.WriteFile(notAKey, []byte("hello"), 0o600))

	tests := []struct {
		name       string
		keyPath    string
		configured bool
		wantErr    bool
	}{
		{name: "unset is simply unanswered", keyPath: ""},
		{name: "whitespace counts as unset", keyPath: "   "},
		{name: "a resolving key is an answer", keyPath: keyPath, configured: true},
		{name: "set but the file is gone", keyPath: missingPath, wantErr: true},
		{name: "set but not a private key", keyPath: notAKey, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			configured, err := configuredSSHKeyPath("git.privateKeyFilePath", tc.keyPath)

			if tc.wantErr {
				require.Error(t, err)
				// The operator has to be able to find the offending line.
				assert.Contains(t, err.Error(), "git.privateKeyFilePath")
				assert.Contains(t, err.Error(), tc.keyPath)
				assert.False(t, configured)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.configured, configured)
		})
	}
}

func TestPlanGitSSHForm(t *testing.T) {
	gitKey := writeSSHKey(t, "id_ed25519")
	deployKey := writeSSHKey(t, "deploy")
	missing := filepath.Join(t.TempDir(), "gone")

	noAgent := &autoDetectedConfig{}
	withAgent := &autoDetectedConfig{SSHAgentAvail: true}

	tests := []struct {
		name      string
		cfg       *PromptedConfig
		detected  *autoDetectedConfig
		want      gitSSHFormPlan
		wantErr   string
		wantErrIn string
	}{
		{
			name:     "nothing configured asks for both",
			cfg:      &PromptedConfig{},
			detected: noAgent,
			want:     gitSSHFormPlan{deployKey: true, gitKey: true},
		},
		{
			// The reported bug: general.yaml names a usable git key, but the
			// deploy key is missing, so the step re-runs. It must ask for the
			// deploy key only — not for the key already on disk.
			name: "a missing deploy key does not drag the git key back",
			cfg: &PromptedConfig{
				SSHKeyPath: gitKey,
			},
			detected: noAgent,
			want:     gitSSHFormPlan{deployKey: true, gitKey: false},
		},
		{
			name: "both configured asks for neither",
			cfg: &PromptedConfig{
				SSHKeyPath:                 gitKey,
				KubeaidConfigDeployKeyPath: deployKey,
			},
			detected: noAgent,
			want:     gitSSHFormPlan{},
		},
		{
			name: "an agent operator is never asked for a git key",
			cfg: &PromptedConfig{
				KubeaidConfigDeployKeyPath: deployKey,
			},
			detected: withAgent,
			want:     gitSSHFormPlan{},
		},
		{
			name:     "an agent operator is still asked for the deploy key",
			cfg:      &PromptedConfig{},
			detected: withAgent,
			want:     gitSSHFormPlan{deployKey: true},
		},
		{
			// Given but not there is a broken config, not an open question.
			name: "a deploy key pointing nowhere fails the run",
			cfg: &PromptedConfig{
				SSHKeyPath:                 gitKey,
				KubeaidConfigDeployKeyPath: missing,
			},
			detected:  noAgent,
			wantErr:   "cluster.argoCD.deployKeys.kubeaidConfig.privateKeyFilePath",
			wantErrIn: missing,
		},
		{
			name: "a git key pointing nowhere fails the run",
			cfg: &PromptedConfig{
				SSHKeyPath:                 missing,
				KubeaidConfigDeployKeyPath: deployKey,
			},
			detected:  noAgent,
			wantErr:   "git.privateKeyFilePath",
			wantErrIn: missing,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := planGitSSHForm(tc.cfg, tc.detected)

			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				assert.Contains(t, err.Error(), tc.wantErrIn)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.want, plan)
		})
	}
}

// A broken key path has to stop the run before the TUI opens, or the failure
// arrives as a prompt the operator cannot answer instead of as an error. This
// test would need a TTY if runGitSSHForm got as far as building the form.
func TestRunGitSSHFormFailsBeforeOpeningTheFormOnABrokenKey(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "gone")

	err := runGitSSHForm(
		&PromptedConfig{KubeaidConfigDeployKeyPath: missing},
		&autoDetectedConfig{SSHAgentAvail: true},
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "cluster.argoCD.deployKeys.kubeaidConfig.privateKeyFilePath")
	assert.NotContains(t, err.Error(), "TTY")
}
