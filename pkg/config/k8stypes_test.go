// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package config_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	coreV1 "k8s.io/api/core/v1"
	k8syaml "sigs.k8s.io/yaml"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
)

// TestMirroredK8sTypesMatchUpstream is the guard on mirroring two core/v1
// types instead of importing them.
//
// pkg/config must stay importable from obmondo-api-go, which has no k8s
// dependencies at all, so HostPathType and Taint are declared locally. The
// usual objection to mirroring is drift; this makes drift a test failure
// rather than something that reaches a cluster.
//
// Marshalled output is what's compared, not field lists: node-group taints
// are serialised into the KubeOne cluster manifest, so what matters is that
// the bytes are indistinguishable from what coreV1 would have produced.
//
// This test imports coreV1 — that's free. Test-only imports do not enter a
// package's dependency graph for its importers, so pkg/config stays clean.
func TestMirroredK8sTypesMatchUpstream(t *testing.T) {
	t.Run("taint marshals identically", func(t *testing.T) {
		cases := []struct {
			name       string
			key, value string
			effect     string
		}{
			{"no schedule with value", "dedicated", "gpu", string(coreV1.TaintEffectNoSchedule)},
			{"no execute", "node.kubernetes.io/unreachable", "", string(coreV1.TaintEffectNoExecute)},
			{"prefer no schedule", "workload", "batch", string(coreV1.TaintEffectPreferNoSchedule)},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				mine, err := k8syaml.Marshal(&config.Taint{
					Key:    tc.key,
					Value:  tc.value,
					Effect: config.TaintEffect(tc.effect),
				})
				require.NoError(t, err)

				// TimeAdded left nil: the kubelet sets it when it applies a
				// taint, it is meaningless in a config file, and coreV1 marks
				// it omitempty — which is why the mirror can omit the field.
				theirs, err := k8syaml.Marshal(&coreV1.Taint{
					Key:    tc.key,
					Value:  tc.value,
					Effect: coreV1.TaintEffect(tc.effect),
				})
				require.NoError(t, err)

				require.Equal(t, string(theirs), string(mine),
					"config.Taint must serialise exactly like coreV1.Taint")
			})
		}
	})

	t.Run("host path type is the same underlying string", func(t *testing.T) {
		for _, v := range []coreV1.HostPathType{
			coreV1.HostPathFileOrCreate,
			coreV1.HostPathDirectoryOrCreate,
			coreV1.HostPathDirectory,
			coreV1.HostPathFile,
		} {
			require.Equal(t, string(v), string(config.HostPathType(v)),
				"config.HostPathType must round-trip coreV1's constants")
		}
	})
}
