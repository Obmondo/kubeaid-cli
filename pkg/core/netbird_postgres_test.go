// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package core

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	crFake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
)

func newPostgresTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	require.NoError(t, coreV1.AddToScheme(s))
	return s
}

func TestBuildPostgresDSN(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		username string
		password string
		want     string
	}{
		{
			name:     "plain alphanumerics",
			username: "netbird",
			password: "abc123",
			want:     "postgresql://netbird:abc123@netbird-pgsql-rw.netbird:5432/netbird",
		},
		{
			name:     "password with @ and / gets percent-encoded",
			username: "netbird",
			password: "p@ss/wo:rd",
			want:     "postgresql://netbird:p%40ss%2Fwo%3Ard@netbird-pgsql-rw.netbird:5432/netbird",
		},
		{
			name:     "empty password still produces valid URI",
			username: "netbird",
			password: "",
			want:     "postgresql://netbird:@netbird-pgsql-rw.netbird:5432/netbird",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, buildPostgresDSN(tc.username, tc.password))
		})
	}
}

func TestPatchNetBirdPostgresDSN_HappyPath(t *testing.T) {
	t.Parallel()

	cl := crFake.NewClientBuilder().
		WithScheme(newPostgresTestScheme(t)).
		WithObjects(
			// CNPG-rendered app credentials Secret.
			&coreV1.Secret{
				ObjectMeta: metaV1.ObjectMeta{
					Namespace: constants.NamespaceNetBird,
					Name:      netBirdPostgresAppSecret,
				},
				Data: map[string][]byte{
					"username": []byte("netbird"),
					"password": []byte("cnpg-generated-pwd"),
				},
			},
			// Pre-existing netbird Secret kubeaid-cli rendered with empty postgresDSN.
			&coreV1.Secret{
				ObjectMeta: metaV1.ObjectMeta{
					Namespace: constants.NamespaceNetBird,
					Name:      constants.SecretNameNetBird,
				},
				Data: map[string][]byte{
					constants.SecretKeyNetBirdPostgresDSN: []byte(""),
				},
			},
		).
		Build()

	require.NoError(t, patchNetBirdPostgresDSN(context.Background(), cl))

	got := &coreV1.Secret{}
	require.NoError(t, cl.Get(context.Background(),
		types.NamespacedName{Namespace: constants.NamespaceNetBird, Name: constants.SecretNameNetBird},
		got,
	))
	assert.Equal(t,
		"postgresql://netbird:cnpg-generated-pwd@netbird-pgsql-rw.netbird:5432/netbird",
		string(got.Data[constants.SecretKeyNetBirdPostgresDSN]),
	)
}

func TestPatchNetBirdPostgresDSN_AppSecretMissing(t *testing.T) {
	t.Parallel()

	// Only the netbird Secret exists; CNPG hasn't rendered the app
	// secret yet. Caller surfaces a clear error so the operator
	// knows to render the netbird-pgsql Cluster CR or wait for
	// CNPG to finish syncing.
	cl := crFake.NewClientBuilder().
		WithScheme(newPostgresTestScheme(t)).
		WithObjects(&coreV1.Secret{
			ObjectMeta: metaV1.ObjectMeta{
				Namespace: constants.NamespaceNetBird,
				Name:      constants.SecretNameNetBird,
			},
		}).
		Build()

	err := patchNetBirdPostgresDSN(context.Background(), cl)
	require.Error(t, err)
	assert.Contains(t, err.Error(), netBirdPostgresAppSecret)
	assert.Contains(t, err.Error(), "netbird-pgsql Cluster CR")
}

func TestPatchNetBirdPostgresDSN_AppSecretMissingPasswordKey(t *testing.T) {
	t.Parallel()

	cl := crFake.NewClientBuilder().
		WithScheme(newPostgresTestScheme(t)).
		WithObjects(&coreV1.Secret{
			ObjectMeta: metaV1.ObjectMeta{
				Namespace: constants.NamespaceNetBird,
				Name:      netBirdPostgresAppSecret,
			},
			Data: map[string][]byte{"username": []byte("netbird")},
		}).
		Build()

	err := patchNetBirdPostgresDSN(context.Background(), cl)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing 'password' key")
}

func TestPatchNetBirdPostgresDSN_NetBirdSecretMissing(t *testing.T) {
	t.Parallel()

	// CNPG app secret is there, but the netbird Secret hasn't been
	// rendered yet (e.g. SealedSecret hasn't synced). The patch
	// can't proceed because there's nothing to update.
	cl := crFake.NewClientBuilder().
		WithScheme(newPostgresTestScheme(t)).
		WithObjects(&coreV1.Secret{
			ObjectMeta: metaV1.ObjectMeta{
				Namespace: constants.NamespaceNetBird,
				Name:      netBirdPostgresAppSecret,
			},
			Data: map[string][]byte{
				"username": []byte("netbird"),
				"password": []byte("pwd"),
			},
		}).
		Build()

	err := patchNetBirdPostgresDSN(context.Background(), cl)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading netbird Secret")
}

func TestPatchNetBirdPostgresDSN_OverwritesExistingValue(t *testing.T) {
	t.Parallel()

	// On a re-run after a CNPG password rotation, the netbird
	// Secret already has a postgresDSN value but it's stale. The
	// patch must overwrite with the current CNPG-derived DSN
	// rather than no-op'ing on "key already present".
	cl := crFake.NewClientBuilder().
		WithScheme(newPostgresTestScheme(t)).
		WithObjects(
			&coreV1.Secret{
				ObjectMeta: metaV1.ObjectMeta{
					Namespace: constants.NamespaceNetBird,
					Name:      netBirdPostgresAppSecret,
				},
				Data: map[string][]byte{
					"username": []byte("netbird"),
					"password": []byte("new-cnpg-pwd"),
				},
			},
			&coreV1.Secret{
				ObjectMeta: metaV1.ObjectMeta{
					Namespace: constants.NamespaceNetBird,
					Name:      constants.SecretNameNetBird,
				},
				Data: map[string][]byte{
					constants.SecretKeyNetBirdPostgresDSN: []byte("postgresql://netbird:stale@old/netbird"),
				},
			},
		).
		Build()

	require.NoError(t, patchNetBirdPostgresDSN(context.Background(), cl))

	got := &coreV1.Secret{}
	require.NoError(t, cl.Get(context.Background(),
		types.NamespacedName{Namespace: constants.NamespaceNetBird, Name: constants.SecretNameNetBird},
		got,
	))
	assert.Contains(t, string(got.Data[constants.SecretKeyNetBirdPostgresDSN]), "new-cnpg-pwd")
}
