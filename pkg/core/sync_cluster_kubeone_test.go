// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package core

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
)

// TestKubeletFlagDriftDetection proves the general.yaml kubelet tuning is compared against a
// host's kubeadm-flags.env exactly the way KubeOne renders it - so 'cluster sync' offers to
// converge a merged-but-never-applied manifest.
func TestKubeletFlagDriftDetection(t *testing.T) {
	maxPods := int32(250)

	realWorldEnvLine := `KUBELET_KUBEADM_ARGS="--container-runtime-endpoint=unix:///run/containerd/containerd.sock --hostname-override=demo-node --max-pods=250 --node-ip=192.0.2.10 --pod-infra-container-image=registry.k8s.io/pause:3.10"`

	t.Run("no kubelet tuning configured means no desired flags", func(t *testing.T) {
		assert.Empty(t, kubeletFlagsFromConfig(nil))
	})

	t.Run("desired flags render exactly like kubeone's kubeadm_env.go", func(t *testing.T) {
		flags := kubeletFlagsFromConfig(&config.BareMetalKubeletConfig{
			MaxPods:        &maxPods,
			SystemReserved: map[string]string{"memory": "256Mi", "cpu": "200m"},
			EvictionHard:   map[string]string{"memory.available": "100Mi"},
		})
		assert.Equal(t, map[string]string{
			"--max-pods":        "250",
			"--system-reserved": "cpu=200m,memory=256Mi",
			"--eviction-hard":   "memory.available<100Mi",
		}, flags)
	})

	testCases := []struct {
		name           string
		desiredFlags   map[string]string
		envContent     string
		expectedDeltas []string
	}{
		{
			name:           "host already carries the desired max-pods",
			desiredFlags:   map[string]string{"--max-pods": "250"},
			envContent:     realWorldEnvLine,
			expectedDeltas: []string{},
		},
		{
			name:           "host still at the kubeadm default",
			desiredFlags:   map[string]string{"--max-pods": "250"},
			envContent:     `KUBELET_KUBEADM_ARGS="--container-runtime-endpoint=unix:///run/containerd/containerd.sock --node-ip=192.0.2.10"`,
			expectedDeltas: []string{"--max-pods : (unset) → 250"},
		},
		{
			name:           "host carries a stale value",
			desiredFlags:   map[string]string{"--max-pods": "250"},
			envContent:     `KUBELET_KUBEADM_ARGS="--max-pods=300"`,
			expectedDeltas: []string{"--max-pods : 300 → 250"},
		},
		{
			name:           "reserved resources match after map ordering",
			desiredFlags:   map[string]string{"--system-reserved": "cpu=200m,memory=256Mi"},
			envContent:     `KUBELET_KUBEADM_ARGS="--system-reserved=cpu=200m,memory=256Mi"`,
			expectedDeltas: []string{},
		},
		{
			name:           "unparsable env content counts as drift",
			desiredFlags:   map[string]string{"--max-pods": "250"},
			envContent:     "garbage without an env assignment",
			expectedDeltas: []string{"--max-pods : (unset) → 250"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(
				t,
				testCase.expectedDeltas,
				kubeletFlagDeltas(testCase.desiredFlags, testCase.envContent),
			)
		})
	}
}
