// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package prompt

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Obmondo/kubeaid-cli/pkg/constants"
)

func TestWorkloadNetBirdSummaryLines(t *testing.T) {
	t.Run("declined the mesh", func(t *testing.T) {
		lines := workloadNetBirdSummaryLines(&PromptedConfig{
			ClusterType: constants.ClusterTypeWorkload,
		})
		joined := strings.Join(lines, "\n")
		assert.Contains(t, joined, "NetBird mesh:  no")
		assert.NotContains(t, joined, "NetBird DNS")
		assert.NotContains(t, joined, "Lockdown")
	})

	t.Run("joined a mesh without a lockdown decision", func(t *testing.T) {
		lines := workloadNetBirdSummaryLines(&PromptedConfig{
			ClusterType:    constants.ClusterTypeWorkload,
			NetBirdDNS:     "netbird.vpn.acme.com",
			NetBirdDNSZone: "mesh.acme.com",
			NetBirdAPIKey:  "nbp_tok",
		})
		joined := strings.Join(lines, "\n")
		assert.Contains(t, joined, "NetBird mesh:  yes")
		assert.Contains(t, joined, "netbird.vpn.acme.com")
		assert.Contains(t, joined, "mesh.acme.com")
		assert.Contains(t, joined, "provided")
		// Lockdown wasn't asked (not bare-metal) — don't imply a decision.
		assert.NotContains(t, joined, "Lockdown")
	})

	t.Run("joined a mesh and chose lockdown", func(t *testing.T) {
		lockdown := true
		lines := workloadNetBirdSummaryLines(&PromptedConfig{
			ClusterType:    constants.ClusterTypeWorkload,
			NetBirdDNS:     "netbird.vpn.acme.com",
			NetBirdDNSZone: "mesh.acme.com",
			NetBirdAPIKey:  "nbp_tok",
			Lockdown:       &lockdown,
		})
		joined := strings.Join(lines, "\n")
		assert.Contains(t, joined, "NetBird mesh:  yes")
		assert.Contains(t, joined, "Lockdown:      yes")
	})
}
