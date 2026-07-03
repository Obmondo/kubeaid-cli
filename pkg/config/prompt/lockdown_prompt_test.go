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

	t.Run("non-bare-metal workload: skipped, form not run", func(t *testing.T) {
		called := false
		runLockdownForm = func(*PromptedConfig) error { called = true; return nil }
		s := &promptSession{
			cfg: &PromptedConfig{
				ClusterType: constants.ClusterTypeWorkload,
				HetznerMode: constants.HetznerModeHCloud,
			},
			state: &promptState{},
		}

		require.NoError(t, s.collectWorkloadLockdownIfNeeded())
		assert.False(t, called, "form must not run for non-bare-metal")
		assert.True(t, s.state.WorkloadLockdown)
		assert.Nil(t, s.cfg.Lockdown)
	})

	t.Run("bare-metal workload: form runs, values trimmed", func(t *testing.T) {
		runLockdownForm = func(c *PromptedConfig) error {
			lockdown := true
			c.Lockdown = &lockdown
			c.NetBirdDNS = "  netbird.vpn.acme.com  "
			c.NetBirdAPIKey = "  nbp_tok  "
			return nil
		}
		s := &promptSession{
			cfg: &PromptedConfig{
				ClusterType: constants.ClusterTypeWorkload,
				HetznerMode: constants.HetznerModeBareMetal,
			},
			state: &promptState{},
		}

		require.NoError(t, s.collectWorkloadLockdownIfNeeded())
		assert.True(t, s.state.WorkloadLockdown)
		require.NotNil(t, s.cfg.Lockdown)
		assert.True(t, *s.cfg.Lockdown)
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
			},
			state: &promptState{},
		}

		require.NoError(t, s.collectWorkloadLockdownIfNeeded())
		assert.False(t, called, "lockdown collection is workload-only")
	})
}
