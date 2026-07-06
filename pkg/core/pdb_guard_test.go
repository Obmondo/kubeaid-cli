// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
	policyV1 "k8s.io/api/policy/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func TestIsArgoCDManaged(t *testing.T) {
	testCases := []struct {
		name        string
		labels      map[string]string
		annotations map[string]string
		expected    bool
	}{
		{
			name:     "untracked PDB",
			labels:   map[string]string{"k8s-app": "kube-dns"},
			expected: false,
		},
		{
			name:     "ArgoCD label tracking (default)",
			labels:   map[string]string{"app.kubernetes.io/instance": "kube-prometheus"},
			expected: true,
		},
		{
			name:     "ArgoCD label tracking (argoproj variant)",
			labels:   map[string]string{"argocd.argoproj.io/instance": "kube-prometheus"},
			expected: true,
		},
		{
			name:        "ArgoCD annotation tracking",
			annotations: map[string]string{"argocd.argoproj.io/tracking-id": "kube-prometheus:policy/PodDisruptionBudget:monitoring/prometheus-adapter"},
			expected:    true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			pdb := &policyV1.PodDisruptionBudget{
				ObjectMeta: metaV1.ObjectMeta{
					Labels:      testCase.labels,
					Annotations: testCase.annotations,
				},
			}
			assert.Equal(t, testCase.expected, isArgoCDManaged(pdb))
		})
	}
}

func TestSanitizePDBForRestore(t *testing.T) {
	maxUnavailable := intstr.FromInt32(1)

	original := policyV1.PodDisruptionBudget{
		ObjectMeta: metaV1.ObjectMeta{
			Namespace:       "kube-system",
			Name:            "coredns",
			Labels:          map[string]string{"k8s-app": "kube-dns"},
			ResourceVersion: "12345",
			UID:             "d4e5f6",
		},
		Spec: policyV1.PodDisruptionBudgetSpec{
			MaxUnavailable: &maxUnavailable,
			Selector: &metaV1.LabelSelector{
				MatchLabels: map[string]string{"k8s-app": "kube-dns"},
			},
		},
	}

	restored := sanitizePDBForRestore(original)

	assert.Equal(t, "kube-system", restored.Namespace)
	assert.Equal(t, "coredns", restored.Name)
	assert.Equal(t, original.Labels, restored.Labels)
	assert.Equal(t, original.Spec, restored.Spec)
	assert.Empty(t, restored.ResourceVersion)
	assert.Empty(t, restored.UID)
}

func TestManualPDBInstructions(t *testing.T) {
	blockingPDBs := []policyV1.PodDisruptionBudget{
		{ObjectMeta: metaV1.ObjectMeta{Namespace: "kube-system", Name: "coredns"}},
		{ObjectMeta: metaV1.ObjectMeta{Namespace: "monitoring", Name: "prometheus-adapter"}},
	}

	instructions := manualPDBInstructions(blockingPDBs)

	assert.Contains(t, instructions, "kubectl -n kube-system delete pdb coredns")
	assert.Contains(t, instructions, "kubectl -n monitoring delete pdb prometheus-adapter")
	assert.Contains(t, instructions, "rerun 'kubeaid-cli cluster upgrade'")

	assert.Equal(
		t,
		"  - kube-system/coredns\n  - monitoring/prometheus-adapter",
		pdbNameList(blockingPDBs),
	)
}
