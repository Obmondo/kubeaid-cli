// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package prompt

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Obmondo/kubeaid-cli/pkg/constants"
)

// TestNetBirdStepDone pins the "is the NetBird prompt step complete?" predicate
// that drives both the resume-fallback state and the collect guard. The key
// case is the legacy workload config (zone set, no Mgmt URL) produced by the
// old unconditional-zone code — it must be reported NOT done so the join gate
// re-opens instead of silently rendering a half netbird block.
func TestNetBirdStepDone(t *testing.T) {
	tests := []struct {
		name string
		cfg  PromptedConfig
		want bool
	}{
		{"vpn with zone", PromptedConfig{ClusterType: constants.ClusterTypeVPN, NetBirdDNSZone: "mesh.acme.com"}, true},
		{"vpn without zone", PromptedConfig{ClusterType: constants.ClusterTypeVPN}, false},
		{"workload joined (dns + zone)", PromptedConfig{ClusterType: constants.ClusterTypeWorkload, NetBirdDNS: "netbird.vpn.acme.com", NetBirdDNSZone: "mesh.acme.com"}, true},
		{"workload declined (neither)", PromptedConfig{ClusterType: constants.ClusterTypeWorkload}, true},
		{"workload legacy zone-only (zone, no dns)", PromptedConfig{ClusterType: constants.ClusterTypeWorkload, NetBirdDNSZone: "mesh.acme.com"}, false},
		{"workload dns-only (dns, no zone)", PromptedConfig{ClusterType: constants.ClusterTypeWorkload, NetBirdDNS: "netbird.vpn.acme.com"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, netBirdStepDone(&tt.cfg))
		})
	}
}

// TestCompletedPromptStateFromValues_NetBird proves the resume fallback (used
// when config files exist but no state file) re-opens the gate for a legacy
// zone-only workload config rather than marking it done.
func TestCompletedPromptStateFromValues_NetBird(t *testing.T) {
	legacy := &PromptedConfig{
		ClusterType:    constants.ClusterTypeWorkload,
		NetBirdDNSZone: "mesh.acme.com", // zone set, no dns — old unconditional-zone config
	}
	assert.False(t, completedPromptStateFromValues(legacy).NetBirdDNSZone,
		"legacy zone-only workload must re-open the join gate on resume")

	joined := &PromptedConfig{
		ClusterType:    constants.ClusterTypeWorkload,
		NetBirdDNS:     "netbird.vpn.acme.com",
		NetBirdDNSZone: "mesh.acme.com",
	}
	assert.True(t, completedPromptStateFromValues(joined).NetBirdDNSZone,
		"a fully-joined workload is done")
}

// TestNetBirdJoinDefault pins the join-gate confirm default: derived from cfg so
// an edit-loop redo (which resets state but keeps cfg) preserves the operator's
// earlier choice instead of flipping a prior decline back to "Yes".
func TestNetBirdJoinDefault(t *testing.T) {
	assert.False(t, netBirdJoinDefault(&PromptedConfig{ClusterType: constants.ClusterTypeWorkload}),
		"fresh / previously-declined workload defaults to No")
	assert.True(t, netBirdJoinDefault(&PromptedConfig{ClusterType: constants.ClusterTypeWorkload, NetBirdDNS: "netbird.vpn.acme.com", NetBirdDNSZone: "mesh.acme.com"}),
		"previously-joined workload defaults to Yes")
}
