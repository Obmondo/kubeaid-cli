// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package reconcile

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/Obmondo/kubeaid-cli/pkg/siem/config"
	"github.com/Obmondo/kubeaid-cli/pkg/siem/report"
	"github.com/Obmondo/kubeaid-cli/pkg/siem/secrets"
)

const irisAPIKey = "IRIS_API_KEY"

func TestSecretKeyStore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	kube := fake.NewClientset()
	ks := secretKeyStore{
		store: secrets.Store{Kube: kube},
		ref:   config.SecretRef{Namespace: "soc", Name: "iris-ai-triage", Key: irisAPIKey},
	}

	got, err := ks.Get(ctx)
	require.NoError(t, err)
	assert.Empty(t, got, "a missing Secret reads as no key")

	a, err := ks.Put(ctx, "k1", true)
	require.NoError(t, err)
	assert.Equal(t, report.ActionCreate, a)
	got, err = ks.Get(ctx)
	require.NoError(t, err)
	assert.Empty(t, got, "dry run must not write")

	_, err = ks.Put(ctx, "k1", false)
	require.NoError(t, err)
	got, err = ks.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, "k1", got)
}
