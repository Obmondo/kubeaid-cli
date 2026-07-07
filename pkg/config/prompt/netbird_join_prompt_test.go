// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package prompt

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-cli/pkg/constants"
)

func TestCollectNetBirdDNSZoneIfNeeded_Workload(t *testing.T) {
	restoreJoin := runNetBirdJoinForm
	restoreZone := runNetBirdZoneForm
	t.Cleanup(func() {
		runNetBirdJoinForm = restoreJoin
		runNetBirdZoneForm = restoreZone
	})

	t.Run("declines the mesh: carried-in values cleared, step marked done", func(t *testing.T) {
		runNetBirdJoinForm = func(*PromptedConfig) (bool, error) { return false, nil }
		zoneCalled := false
		runNetBirdZoneForm = func(*PromptedConfig) error { zoneCalled = true; return nil }

		s := &promptSession{
			// Stale values as if carried in from a prior join on an edit-loop redo.
			cfg: &PromptedConfig{
				ClusterType:    constants.ClusterTypeWorkload,
				NetBirdDNS:     "netbird.old.acme.com",
				NetBirdDNSZone: "old.acme.com",
				NetBirdAPIKey:  "nbp_old",
			},
			state: &promptState{},
		}

		require.NoError(t, s.collectNetBirdDNSZoneIfNeeded())
		assert.Empty(t, s.cfg.NetBirdDNS, "decline clears carried-in Mgmt URL")
		assert.Empty(t, s.cfg.NetBirdDNSZone, "decline clears carried-in zone")
		assert.Empty(t, s.cfg.NetBirdAPIKey, "decline clears carried-in key")
		assert.True(t, s.state.NetBirdDNSZone, "step must be marked done even on decline")
		assert.False(t, zoneCalled, "workload must not run the vpn zone form")
	})

	t.Run("joins the mesh: mgmt URL + zone + key collected and trimmed", func(t *testing.T) {
		runNetBirdJoinForm = func(c *PromptedConfig) (bool, error) {
			c.NetBirdDNS = "  netbird.vpn.acme.com  "
			c.NetBirdDNSZone = "  mesh.acme.com  "
			c.NetBirdAPIKey = "  nbp_tok  "
			return true, nil
		}
		runNetBirdZoneForm = func(*PromptedConfig) error { return nil }

		s := &promptSession{
			cfg:   &PromptedConfig{ClusterType: constants.ClusterTypeWorkload},
			state: &promptState{},
		}

		require.NoError(t, s.collectNetBirdDNSZoneIfNeeded())
		assert.Equal(t, "netbird.vpn.acme.com", s.cfg.NetBirdDNS)
		assert.Equal(t, "mesh.acme.com", s.cfg.NetBirdDNSZone)
		assert.Equal(t, "nbp_tok", s.cfg.NetBirdAPIKey)
		assert.True(t, s.state.NetBirdDNSZone)
	})

	t.Run("already done: form not re-run (resumed decline stays declined)", func(t *testing.T) {
		called := false
		runNetBirdJoinForm = func(*PromptedConfig) (bool, error) { called = true; return false, nil }

		s := &promptSession{
			cfg:   &PromptedConfig{ClusterType: constants.ClusterTypeWorkload},
			state: &promptState{NetBirdDNSZone: true},
		}

		require.NoError(t, s.collectNetBirdDNSZoneIfNeeded())
		assert.False(t, called, "a completed netbird step (incl. decline) must not re-prompt")
	})
}

func TestNetBirdZoneValidator(t *testing.T) {
	cfg := &PromptedConfig{
		NetBirdDNS:           "netbird.vpn.acme.com",
		ControlPlaneEndpoint: "api.vpn.acme.com",
	}
	validate := netBirdZoneValidator(cfg)

	assert.Error(t, validate(""), "empty zone rejected")
	assert.Error(t, validate("api.vpn.acme.com"), "must differ from the control-plane endpoint")
	assert.Error(t, validate("netbird.vpn.acme.com"), "must differ from the Mgmt domain")
	assert.NoError(t, validate("mesh.acme.com"), "a distinct domain is accepted")
}

func TestCollectNetBirdDNSZoneIfNeeded_VPN(t *testing.T) {
	restoreJoin := runNetBirdJoinForm
	restoreZone := runNetBirdZoneForm
	t.Cleanup(func() {
		runNetBirdJoinForm = restoreJoin
		runNetBirdZoneForm = restoreZone
	})

	joinCalled := false
	runNetBirdJoinForm = func(*PromptedConfig) (bool, error) { joinCalled = true; return false, nil }
	runNetBirdZoneForm = func(c *PromptedConfig) error {
		c.NetBirdDNSZone = "mesh.acme.com"
		return nil
	}

	s := &promptSession{
		cfg:   &PromptedConfig{ClusterType: constants.ClusterTypeVPN},
		state: &promptState{},
	}

	require.NoError(t, s.collectNetBirdDNSZoneIfNeeded())
	assert.Equal(t, "mesh.acme.com", s.cfg.NetBirdDNSZone, "vpn still collects the required zone")
	assert.True(t, s.state.NetBirdDNSZone)
	assert.False(t, joinCalled, "vpn must not run the workload join gate")
}
