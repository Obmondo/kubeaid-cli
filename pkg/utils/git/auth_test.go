// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package git

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
)

// TestGitAuthModeFor pins both routing paths — file-based vs SSH-agent —
// without standing up an actual SSH agent or generating a keypair on disk.
// The selection logic is a pure function over GitConfig, so a table test
// of the predicate combinations covers the whole contract.
//
// SSHKeyPairConfig is a *pointer* on GitConfig; nil means "operator gave
// neither a path nor opted into the agent" — we still default to agent
// in that case (matches the operator's typical state when only a yaml
// file's `useSSHAgent: true` is set, since that hydrates the embedded
// pointer with just the bool).
func TestGitAuthModeFor(t *testing.T) {
	tests := []struct {
		name    string
		gitCfg  config.GitConfig
		want    gitAuthMode
		wantStr string
	}{
		{
			name:    "no SSHKeyPairConfig pointer set: agent (default)",
			gitCfg:  config.GitConfig{},
			want:    gitAuthModeAgent,
			wantStr: "agent",
		},
		{
			name: "private key file path with UseSSHAgent=false: private key file",
			gitCfg: config.GitConfig{
				SSHKeyPairConfig: &config.SSHKeyPairConfig{
					PrivateKeyFilePath: "/home/op/.ssh/id_ed25519_kubeaid",
					UseSSHAgent:        false,
				},
			},
			want:    gitAuthModePrivateKeyFile,
			wantStr: "private-key-file",
		},
		{
			name: "UseSSHAgent=true: agent (even when path is also set)",
			gitCfg: config.GitConfig{
				SSHKeyPairConfig: &config.SSHKeyPairConfig{
					PrivateKeyFilePath: "/home/op/.ssh/id_ed25519_kubeaid",
					UseSSHAgent:        true,
				},
			},
			want:    gitAuthModeAgent,
			wantStr: "agent (UseSSHAgent wins)",
		},
		{
			name: "UseSSHAgent=true alone: agent",
			gitCfg: config.GitConfig{
				SSHKeyPairConfig: &config.SSHKeyPairConfig{
					UseSSHAgent: true,
				},
			},
			want:    gitAuthModeAgent,
			wantStr: "agent",
		},
		{
			name: "SSHKeyPairConfig set but path empty + UseSSHAgent=false: agent (no path → can't use file)",
			gitCfg: config.GitConfig{
				SSHKeyPairConfig: &config.SSHKeyPairConfig{
					PrivateKeyFilePath: "",
					UseSSHAgent:        false,
				},
			},
			want:    gitAuthModeAgent,
			wantStr: "agent (empty path falls through)",
		},
		{
			name: "private key path with whitespace-only is treated as a path (no trim)",
			gitCfg: config.GitConfig{
				SSHKeyPairConfig: &config.SSHKeyPairConfig{
					PrivateKeyFilePath: " ", // not empty → still routes to file
					UseSSHAgent:        false,
				},
			},
			want:    gitAuthModePrivateKeyFile,
			wantStr: "private-key-file (we don't trim — caller's responsibility)",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := gitAuthModeFor(tc.gitCfg)
			assert.Equal(t, tc.want, got,
				"got %d (%s) want %d", got, tc.wantStr, tc.want)
		})
	}
}
