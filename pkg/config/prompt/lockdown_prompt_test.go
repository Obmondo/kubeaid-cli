// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package prompt

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-cli/pkg/constants"
)

func TestCollectWorkloadLockdownIfNeeded(t *testing.T) {
	restore := runLockdownForm
	t.Cleanup(func() { runLockdownForm = restore })

	t.Run("not joining a mesh: lockdown skipped even on bare-metal", func(t *testing.T) {
		called := false
		runLockdownForm = func(*PromptedConfig) error { called = true; return nil }
		s := &promptSession{
			cfg: &PromptedConfig{
				ClusterType: constants.ClusterTypeWorkload,
				HetznerMode: constants.HetznerModeBareMetal,
				// NetBirdDNS empty — the cluster declined the mesh, so a
				// public-NIC lockdown would only strand kubectl.
			},
			state: &promptState{},
		}

		require.NoError(t, s.collectWorkloadLockdownIfNeeded())
		assert.False(t, called, "lockdown is meaningless without a mesh to fall back to")
		assert.True(t, s.state.WorkloadLockdown)
		assert.Nil(t, s.cfg.Lockdown)
	})

	t.Run("joining + non-bare-metal workload: skipped, form not run", func(t *testing.T) {
		called := false
		runLockdownForm = func(*PromptedConfig) error { called = true; return nil }
		s := &promptSession{
			cfg: &PromptedConfig{
				ClusterType: constants.ClusterTypeWorkload,
				HetznerMode: constants.HetznerModeHCloud,
				NetBirdDNS:  "netbird.vpn.acme.com",
			},
			state: &promptState{},
		}

		require.NoError(t, s.collectWorkloadLockdownIfNeeded())
		assert.False(t, called, "form must not run for non-bare-metal")
		assert.True(t, s.state.WorkloadLockdown)
		assert.Nil(t, s.cfg.Lockdown)
	})

	t.Run("joining + bare-metal workload: form runs, sets lockdown only", func(t *testing.T) {
		runLockdownForm = func(c *PromptedConfig) error {
			lockdown := true
			c.Lockdown = &lockdown
			return nil
		}
		s := &promptSession{
			cfg: &PromptedConfig{
				ClusterType:   constants.ClusterTypeWorkload,
				HetznerMode:   constants.HetznerModeBareMetal,
				NetBirdDNS:    "netbird.vpn.acme.com",
				NetBirdAPIKey: "nbp_tok",
			},
			state: &promptState{},
		}

		require.NoError(t, s.collectWorkloadLockdownIfNeeded())
		assert.True(t, s.state.WorkloadLockdown)
		require.NotNil(t, s.cfg.Lockdown)
		assert.True(t, *s.cfg.Lockdown)
		// The join form already collected these; lockdown leaves them intact.
		assert.Equal(t, "netbird.vpn.acme.com", s.cfg.NetBirdDNS)
		assert.Equal(t, "nbp_tok", s.cfg.NetBirdAPIKey)
	})

	t.Run("vpn cluster: skipped entirely", func(t *testing.T) {
		called := false
		runLockdownForm = func(*PromptedConfig) error { called = true; return nil }
		s := &promptSession{
			cfg: &PromptedConfig{
				ClusterType: constants.ClusterTypeVPN,
				HetznerMode: constants.HetznerModeBareMetal,
				NetBirdDNS:  "netbird.vpn.acme.com",
			},
			state: &promptState{},
		}

		require.NoError(t, s.collectWorkloadLockdownIfNeeded())
		assert.False(t, called, "lockdown collection is workload-only")
	})
}
