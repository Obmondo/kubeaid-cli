// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package core

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// capiValues renders a minimal values-capi-cluster.yaml carrying only the fields
// planCapiValuesSync reads.
type capiValues struct {
	rotation   bool
	imageName  string
	cpType     string
	cpReplicas int
	nodeGroups map[string]string
}

func (v capiValues) render() []byte {
	imageName := v.imageName
	if len(imageName) == 0 {
		imageName = "ubuntu-24.04"
	}

	cpType := v.cpType
	if len(cpType) == 0 {
		cpType = "cpx32"
	}

	cpReplicas := v.cpReplicas
	if cpReplicas == 0 {
		cpReplicas = 1
	}

	var nodeGroups strings.Builder
	for _, name := range sortedKeys(v.nodeGroups) {
		fmt.Fprintf(&nodeGroups, "\n    - name: %s\n      machineType: %s", name, v.nodeGroups[name])
	}
	if nodeGroups.Len() == 0 {
		nodeGroups.WriteString(" []")
	}

	return fmt.Appendf(nil, `global:
  clusterName: demo
  machineTemplateRotation: %t
hetzner:
  mode: hcloud
  hcloud:
    imageName: %s
  controlPlane:
    hcloud:
      machineType: %s
      replicas: %d
  nodeGroups:
    hcloud:%s
`, v.rotation, imageName, cpType, cpReplicas, nodeGroups.String())
}

func TestPlanCapiValuesSync(t *testing.T) {
	t.Parallel()

	baseline := capiValues{cpType: "cpx32", cpReplicas: 1, nodeGroups: map[string]string{"md-0": "cpx41"}}

	testCases := []struct {
		name string

		before, after capiValues

		wantRolls           bool
		wantRollingChanges  []string
		wantReplicaChange   string
		wantRefusalContains string
		wantWarningContains string
	}{
		{
			name:   "no change is not a roll and not a scale",
			before: baseline,
			after:  baseline,
		},
		{
			name:              "replicas only scales out, rotation irrelevant",
			before:            baseline,
			after:             capiValues{cpType: "cpx32", cpReplicas: 3, nodeGroups: baseline.nodeGroups},
			wantReplicaChange: "1 -> 3",
			wantRolls:         false,
		},
		{
			name:   "machineType change without rotation is refused as a silent no-op",
			before: baseline,
			after:  capiValues{cpType: "cpx22", cpReplicas: 1, nodeGroups: baseline.nodeGroups},
			wantRollingChanges: []string{
				"controlPlane.hcloud.machineType : cpx32 -> cpx22",
			},
			// Nothing rolls: the template keeps its fixed name, so ClusterAPI never
			// notices. That is precisely why the change is refused.
			wantRolls:           false,
			wantRefusalContains: "nothing would roll",
		},
		{
			name:   "machineType change with rotation rolls, and warns below 3 replicas",
			before: capiValues{rotation: true, cpType: "cpx32", cpReplicas: 1, nodeGroups: baseline.nodeGroups},
			after:  capiValues{rotation: true, cpType: "cpx22", cpReplicas: 1, nodeGroups: baseline.nodeGroups},
			wantRollingChanges: []string{
				"controlPlane.hcloud.machineType : cpx32 -> cpx22",
			},
			wantRolls:           true,
			wantWarningContains: "losing quorum",
		},
		{
			name:      "machineType change with rotation at 3 replicas rolls without warning",
			before:    capiValues{rotation: true, cpType: "cpx32", cpReplicas: 3, nodeGroups: baseline.nodeGroups},
			after:     capiValues{rotation: true, cpType: "cpx22", cpReplicas: 3, nodeGroups: baseline.nodeGroups},
			wantRolls: true,
		},
		{
			name:                "rolling and scaling together is refused",
			before:              capiValues{rotation: true, cpType: "cpx32", cpReplicas: 1, nodeGroups: baseline.nodeGroups},
			after:               capiValues{rotation: true, cpType: "cpx22", cpReplicas: 3, nodeGroups: baseline.nodeGroups},
			wantRolls:           true,
			wantReplicaChange:   "1 -> 3",
			wantRefusalContains: "Split it into two syncs",
		},
		{
			name:                "enabling rotation alone rolls, and refuses to also scale",
			before:              capiValues{cpType: "cpx32", cpReplicas: 1, nodeGroups: baseline.nodeGroups},
			after:               capiValues{rotation: true, cpType: "cpx32", cpReplicas: 3, nodeGroups: baseline.nodeGroups},
			wantRolls:           true,
			wantReplicaChange:   "1 -> 3",
			wantRefusalContains: "Split it into two syncs",
		},
		{
			name:      "enabling rotation at 3 replicas rolls once, cleanly",
			before:    capiValues{cpType: "cpx32", cpReplicas: 3, nodeGroups: baseline.nodeGroups},
			after:     capiValues{rotation: true, cpType: "cpx32", cpReplicas: 3, nodeGroups: baseline.nodeGroups},
			wantRolls: true,
		},
		{
			name:   "imageName change rotates the template",
			before: capiValues{rotation: true, imageName: "ubuntu-24.04", cpReplicas: 3, nodeGroups: baseline.nodeGroups},
			after:  capiValues{rotation: true, imageName: "ubuntu-26.04", cpReplicas: 3, nodeGroups: baseline.nodeGroups},
			wantRollingChanges: []string{
				"hcloud.imageName : ubuntu-24.04 -> ubuntu-26.04",
			},
			wantRolls: true,
		},
		{
			name:   "node-group machineType change rotates only that node group",
			before: capiValues{rotation: true, cpReplicas: 3, nodeGroups: map[string]string{"md-0": "cpx41", "md-1": "cpx31"}},
			after:  capiValues{rotation: true, cpReplicas: 3, nodeGroups: map[string]string{"md-0": "cpx51", "md-1": "cpx31"}},
			wantRollingChanges: []string{
				"nodeGroups.hcloud[md-0].machineType : cpx41 -> cpx51",
			},
			wantRolls: true,
		},
		{
			name:      "a newly added node group is not a rolling change",
			before:    capiValues{rotation: true, cpReplicas: 3, nodeGroups: map[string]string{"md-0": "cpx41"}},
			after:     capiValues{rotation: true, cpReplicas: 3, nodeGroups: map[string]string{"md-0": "cpx41", "md-new": "cpx31"}},
			wantRolls: false,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			plan, err := planCapiValuesSync(testCase.before.render(), testCase.after.render())
			require.NoError(t, err)

			assert.Equal(t, testCase.wantRolls, plan.rolls(), "whether ClusterAPI replaces machines")
			assert.Equal(t, testCase.wantReplicaChange, plan.ReplicaChange, "control-plane scale")

			if testCase.wantRollingChanges != nil {
				assert.Equal(t, testCase.wantRollingChanges, plan.RollingChanges)
			}

			refusals := strings.Join(plan.Refusals(), "\n")
			if len(testCase.wantRefusalContains) == 0 {
				assert.Empty(t, plan.Refusals(), "expected the change to be allowed")
			} else {
				assert.Contains(t, refusals, testCase.wantRefusalContains)
			}

			warnings := strings.Join(plan.Warnings(), "\n")
			if len(testCase.wantWarningContains) == 0 {
				assert.Empty(t, plan.Warnings(), "expected no warning")
			} else {
				assert.Contains(t, warnings, testCase.wantWarningContains)
			}
		})
	}
}

// The netbird-obmondo-com change, staged the way the refusals force you to stage it.
func TestPlanCapiValuesSyncNetbirdTwoPhase(t *testing.T) {
	t.Parallel()

	oneReplicaCPX32 := capiValues{cpType: "cpx32", cpReplicas: 1}
	threeReplicaCPX32 := capiValues{cpType: "cpx32", cpReplicas: 3}
	threeReplicaCPX22 := capiValues{rotation: true, cpType: "cpx22", cpReplicas: 3}

	t.Run("phase 1 : scale 1 -> 3 on the existing type, members join etcd", func(t *testing.T) {
		t.Parallel()

		plan, err := planCapiValuesSync(oneReplicaCPX32.render(), threeReplicaCPX32.render())
		require.NoError(t, err)

		assert.False(t, plan.rolls(), "a pure scale-out must not replace any machine")
		assert.Equal(t, "1 -> 3", plan.ReplicaChange)
		assert.Empty(t, plan.Refusals())
		assert.Empty(t, plan.Warnings())
	})

	t.Run("phase 2 : enable rotation and re-type, rolling with quorum to spare", func(t *testing.T) {
		t.Parallel()

		plan, err := planCapiValuesSync(threeReplicaCPX32.render(), threeReplicaCPX22.render())
		require.NoError(t, err)

		assert.True(t, plan.rolls())
		assert.True(t, plan.RotationNewlyEnabled)
		assert.Empty(t, plan.ReplicaChange, "replicas must already be settled before re-typing")
		assert.Empty(t, plan.Refusals())
		assert.Empty(t, plan.Warnings(), "3 replicas is enough to roll safely")
	})

	t.Run("doing both at once is refused", func(t *testing.T) {
		t.Parallel()

		plan, err := planCapiValuesSync(oneReplicaCPX32.render(), threeReplicaCPX22.render())
		require.NoError(t, err)

		assert.Contains(t, strings.Join(plan.Refusals(), "\n"), "Split it into two syncs")
	})
}
