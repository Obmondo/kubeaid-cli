// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package config

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Obmondo/kubeaid-cli/pkg/constants"
)

// bareMetalHosts builds a slice of n hosts — only the count matters for the
// node-count helpers, so the host fields are left zero.
func bareMetalHosts(n int) []*HetznerBareMetalHost {
	out := make([]*HetznerBareMetalHost, n)
	for i := range out {
		out[i] = &HetznerBareMetalHost{}
	}
	return out
}

// hetznerConfig builds a *GeneralConfig with a Hetzner cloud of the given mode,
// controlPlaneHosts bare-metal control-plane hosts, and one worker node-group
// per entry in workerGroupHosts (each carrying that many hosts). The
// control-plane hosts are set so tests can prove they're NOT counted.
func hetznerConfig(mode string, controlPlaneHosts int, workerGroupHosts ...int) *GeneralConfig {
	hetzner := &HetznerConfig{Mode: mode}

	if controlPlaneHosts > 0 {
		hetzner.ControlPlane.BareMetal = &HetznerBareMetalControlPlane{
			BareMetalHosts: bareMetalHosts(controlPlaneHosts),
		}
	}
	for _, n := range workerGroupHosts {
		hetzner.NodeGroups.BareMetal = append(hetzner.NodeGroups.BareMetal,
			&HetznerBareMetalNodeGroup{BareMetalHosts: bareMetalHosts(n)})
	}

	return &GeneralConfig{Cloud: CloudConfig{Hetzner: hetzner}}
}

func TestHetznerBareMetalWorkerNodeCount(t *testing.T) {
	original := ParsedGeneralConfig
	defer func() { ParsedGeneralConfig = original }()

	// withNilNodeGroup appends a nil bare-metal node-group, which must be
	// skipped rather than panicked on.
	withNilNodeGroup := hetznerConfig(constants.HetznerModeBareMetal, 0, 2)
	withNilNodeGroup.Cloud.Hetzner.NodeGroups.BareMetal = append(
		withNilNodeGroup.Cloud.Hetzner.NodeGroups.BareMetal, nil)

	tests := []struct {
		name string
		cfg  *GeneralConfig
		want int
	}{
		{"nil hetzner", &GeneralConfig{Cloud: CloudConfig{Hetzner: nil}}, 0},
		// Control-plane hosts never count, even with no workers.
		{"control-plane only", hetznerConfig(constants.HetznerModeBareMetal, 3), 0},
		{"workers across two node-groups", hetznerConfig(constants.HetznerModeBareMetal, 0, 2, 1), 3},
		// Control-plane present but ignored — only the 2 workers count.
		{"control-plane plus workers counts workers only", hetznerConfig(constants.HetznerModeBareMetal, 3, 2), 2},
		{"hybrid worker node-groups", hetznerConfig(constants.HetznerModeHybrid, 0, 2), 2},
		{"nil worker node-group element is skipped", withNilNodeGroup, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ParsedGeneralConfig = tt.cfg
			assert.Equal(t, tt.want, HetznerBareMetalWorkerNodeCount())
		})
	}
}

func TestRookCephEnabled(t *testing.T) {
	original := ParsedGeneralConfig
	defer func() { ParsedGeneralConfig = original }()

	tests := []struct {
		name string
		cfg  *GeneralConfig
		want bool
	}{
		{"nil hetzner", &GeneralConfig{Cloud: CloudConfig{Hetzner: nil}}, false},
		// HCloud is never bare-metal, so Ceph is off regardless of host count.
		{"hcloud is never eligible", hetznerConfig(constants.HetznerModeHCloud, 0, 5), false},
		{"bare-metal below threshold", hetznerConfig(constants.HetznerModeBareMetal, 0, 2), false},
		{"bare-metal exactly at threshold", hetznerConfig(constants.HetznerModeBareMetal, 0, 3), true},
		// 3 control-plane hosts can't host Ceph, so 2 workers still gates it off.
		{"control-plane nodes don't satisfy the threshold", hetznerConfig(constants.HetznerModeBareMetal, 3, 2), false},
		{"bare-metal control-plane plus enough workers", hetznerConfig(constants.HetznerModeBareMetal, 3, 3), true},
		{"hybrid with enough bare-metal workers", hetznerConfig(constants.HetznerModeHybrid, 0, 3), true},
		{"hybrid below threshold", hetznerConfig(constants.HetznerModeHybrid, 0, 2), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ParsedGeneralConfig = tt.cfg
			assert.Equal(t, tt.want, RookCephEnabled())
		})
	}
}
