// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package keycloak

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	crFake "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()

	s := runtime.NewScheme()
	require.NoError(t, coreV1.AddToScheme(s))
	return s
}

func TestGetOrGenerateClientSecret_NilClient(t *testing.T) {
	t.Parallel()

	// When the cluster client isn't available yet (pre-bootstrap),
	// the helper falls back to generating a fresh secret.
	got, err := GetOrGenerateClientSecret(context.Background(), nil, "ns", "name", "key")
	require.NoError(t, err)
	assert.Len(t, got, passwordLength)
}

func TestGetOrGenerateClientSecret_ReusesExisting(t *testing.T) {
	t.Parallel()

	const persisted = "persisted-on-prior-run"

	cl := crFake.NewClientBuilder().
		WithScheme(newTestScheme(t)).
		WithObjects(&coreV1.Secret{
			ObjectMeta: metaV1.ObjectMeta{Namespace: "netbird", Name: "netbird-keycloak"},
			Data:       map[string][]byte{"OIDC_CLIENT_SECRET": []byte(persisted)},
		}).
		Build()

	got, err := GetOrGenerateClientSecret(context.Background(), cl,
		"netbird", "netbird-keycloak", "OIDC_CLIENT_SECRET")
	require.NoError(t, err)
	assert.Equal(t, persisted, got, "must read the persisted value verbatim")
}

func TestGetOrGenerateClientSecret_GeneratesWhenSecretMissing(t *testing.T) {
	t.Parallel()

	cl := crFake.NewClientBuilder().WithScheme(newTestScheme(t)).Build()

	got, err := GetOrGenerateClientSecret(context.Background(), cl, "ns", "name", "key")
	require.NoError(t, err)
	assert.Len(t, got, passwordLength)
}

func TestGetOrGenerateClientSecret_GeneratesWhenKeyMissing(t *testing.T) {
	t.Parallel()

	cl := crFake.NewClientBuilder().
		WithScheme(newTestScheme(t)).
		WithObjects(&coreV1.Secret{
			ObjectMeta: metaV1.ObjectMeta{Namespace: "ns", Name: "name"},
			Data:       map[string][]byte{"different-key": []byte("hi")},
		}).
		Build()

	got, err := GetOrGenerateClientSecret(context.Background(), cl, "ns", "name", "key")
	require.NoError(t, err)
	assert.Len(t, got, passwordLength)
}
