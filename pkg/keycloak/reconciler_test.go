// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package keycloak

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testRealm = "acme"

// seedRealm calls ReconcileRealm and resets the write counter so
// subsequent assertions can focus on the function under test.
func seedRealm(t *testing.T, r *Reconciler, fake *fakeKeycloak) {
	t.Helper()
	require.NoError(t, r.ReconcileRealm(context.Background(), testRealm))
	fake.writeCount = 0
}

func TestReconcileRealm(t *testing.T) {
	t.Parallel()

	r, fake := newTestReconciler(t)

	require.NoError(t, r.ReconcileRealm(context.Background(), testRealm))
	assert.Equal(t, 1, fake.writeCount, "first reconcile must create the realm")

	require.NoError(t, r.ReconcileRealm(context.Background(), testRealm))
	assert.Equal(t, 1, fake.writeCount, "second reconcile must be a no-op")
}

func TestReconcileClient_Public(t *testing.T) {
	t.Parallel()

	r, fake := newTestReconciler(t)
	seedRealm(t, r, fake)

	spec := ClientSpec{
		ClientID:            "kubernetes-acme",
		PublicClient:        true,
		StandardFlowEnabled: true,
		RedirectURIs:        []string{"http://localhost:8000"},
	}

	secret, err := r.ReconcileClient(context.Background(), testRealm, spec)
	require.NoError(t, err)
	assert.Equal(t, "", secret, "public clients have no secret")
	assert.Equal(t, 1, fake.writeCount)

	secret, err = r.ReconcileClient(context.Background(), testRealm, spec)
	require.NoError(t, err)
	assert.Equal(t, "", secret)
	assert.Equal(t, 1, fake.writeCount, "second reconcile must be a no-op")
}

func TestReconcileClient_Confidential_GeneratedSecret(t *testing.T) {
	t.Parallel()

	r, fake := newTestReconciler(t)
	seedRealm(t, r, fake)

	spec := ClientSpec{
		ClientID:               "netbird-backend",
		PublicClient:           false,
		ServiceAccountsEnabled: true,
	}

	first, err := r.ReconcileClient(context.Background(), testRealm, spec)
	require.NoError(t, err)
	assert.NotEmpty(t, first, "confidential client must surface its secret")
	assert.Equal(t, 1, fake.writeCount)

	second, err := r.ReconcileClient(context.Background(), testRealm, spec)
	require.NoError(t, err)
	assert.Equal(t, first, second,
		"second reconcile must return the same secret without writing")
	assert.Equal(t, 1, fake.writeCount)
}

func TestReconcileClient_Confidential_PreSetSecret(t *testing.T) {
	t.Parallel()

	r, fake := newTestReconciler(t)
	seedRealm(t, r, fake)

	const preSet = "operator-supplied-secret"
	spec := ClientSpec{
		ClientID:     "netbird-backend",
		PublicClient: false,
		Secret:       preSet,
	}

	got, err := r.ReconcileClient(context.Background(), testRealm, spec)
	require.NoError(t, err)
	assert.Equal(t, preSet, got,
		"pre-set secret must be returned verbatim — kubeaid-cli avoids the second round-trip")
	assert.Equal(t, 1, fake.writeCount)
}

func TestReconcileClientScope(t *testing.T) {
	t.Parallel()

	r, fake := newTestReconciler(t)
	seedRealm(t, r, fake)

	spec := ClientScopeSpec{Name: "api", Protocol: "openid-connect"}

	require.NoError(t, r.ReconcileClientScope(context.Background(), testRealm, spec))
	assert.Equal(t, 1, fake.writeCount)

	require.NoError(t, r.ReconcileClientScope(context.Background(), testRealm, spec))
	assert.Equal(t, 1, fake.writeCount, "second reconcile must be a no-op")
}

func TestAssignClientDefaultScopes(t *testing.T) {
	t.Parallel()

	r, fake := newTestReconciler(t)
	seedRealm(t, r, fake)

	require.NoError(t, r.ReconcileClientScope(
		context.Background(), testRealm,
		ClientScopeSpec{Name: "api", Protocol: "openid-connect"},
	))
	require.NoError(t, r.ReconcileClientScope(
		context.Background(), testRealm,
		ClientScopeSpec{Name: "groups", Protocol: "openid-connect"},
	))
	_, err := r.ReconcileClient(context.Background(), testRealm, ClientSpec{
		ClientID:     "netbird-client",
		PublicClient: true,
	})
	require.NoError(t, err)
	fake.writeCount = 0

	require.NoError(t, r.AssignClientDefaultScopes(
		context.Background(), testRealm, "netbird-client",
		[]string{"api", "groups"},
	))
	assert.Equal(t, 2, fake.writeCount, "two scopes assigned, two writes")

	// Re-assign one of them — should be a no-op (already a default).
	require.NoError(t, r.AssignClientDefaultScopes(
		context.Background(), testRealm, "netbird-client",
		[]string{"api"},
	))
	assert.Equal(t, 2, fake.writeCount,
		"re-assigning an existing default must produce no writes")
}

func TestAssignClientDefaultScopes_UnknownScope(t *testing.T) {
	t.Parallel()

	r, fake := newTestReconciler(t)
	seedRealm(t, r, fake)

	_, err := r.ReconcileClient(context.Background(), testRealm, ClientSpec{
		ClientID:     "netbird-client",
		PublicClient: true,
	})
	require.NoError(t, err)

	err = r.AssignClientDefaultScopes(
		context.Background(), testRealm, "netbird-client",
		[]string{"never-created"},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `client scope "never-created" not found`)
}

func TestReconcileProtocolMapperOnClientScope(t *testing.T) {
	t.Parallel()

	r, fake := newTestReconciler(t)
	seedRealm(t, r, fake)

	require.NoError(t, r.ReconcileClientScope(
		context.Background(), testRealm,
		ClientScopeSpec{Name: "api", Protocol: "openid-connect"},
	))
	fake.writeCount = 0

	spec := ProtocolMapperSpec{
		Name:           "Audience for NetBird Management API",
		Protocol:       "openid-connect",
		ProtocolMapper: "oidc-audience-mapper",
		Config: map[string]string{
			"included.client.audience": "netbird-mgmt",
			"id.token.claim":           "true",
			"access.token.claim":       "true",
		},
	}

	require.NoError(t, r.ReconcileProtocolMapperOnClientScope(
		context.Background(), testRealm, "api", spec,
	))
	assert.Equal(t, 1, fake.writeCount)

	require.NoError(t, r.ReconcileProtocolMapperOnClientScope(
		context.Background(), testRealm, "api", spec,
	))
	assert.Equal(t, 1, fake.writeCount, "second reconcile must be a no-op")
}

func TestAssignClientServiceAccountRole(t *testing.T) {
	t.Parallel()

	r, fake := newTestReconciler(t)
	seedRealm(t, r, fake)

	// netbird-backend is the source: confidential client with a
	// service-account user.
	_, err := r.ReconcileClient(context.Background(), testRealm, ClientSpec{
		ClientID:               "netbird-backend",
		PublicClient:           false,
		ServiceAccountsEnabled: true,
	})
	require.NoError(t, err)

	// realm-management is the target — pre-seeded with view-users
	// in the fake's clientRolesFixture.
	_, err = r.ReconcileClient(context.Background(), testRealm, ClientSpec{
		ClientID:     "realm-management",
		PublicClient: false,
	})
	require.NoError(t, err)
	fake.writeCount = 0

	require.NoError(t, r.AssignClientServiceAccountRole(
		context.Background(), testRealm,
		"netbird-backend", "realm-management", "view-users",
	))
	assert.Equal(t, 1, fake.writeCount)

	require.NoError(t, r.AssignClientServiceAccountRole(
		context.Background(), testRealm,
		"netbird-backend", "realm-management", "view-users",
	))
	assert.Equal(t, 1, fake.writeCount, "second reconcile must be a no-op")
}

func TestReconcileUser(t *testing.T) {
	t.Parallel()

	r, fake := newTestReconciler(t)
	seedRealm(t, r, fake)

	spec := UserSpec{
		Username:  "alice",
		Email:     "alice@acme.com",
		FirstName: "Alice",
		LastName:  "Anderson",
		Enabled:   true,
	}

	require.NoError(t, r.ReconcileUser(context.Background(), testRealm, spec, "initial-pw"))
	assert.Equal(t, 2, fake.writeCount,
		"first reconcile creates the user and sets the password (two writes)")

	require.NoError(t, r.ReconcileUser(context.Background(), testRealm, spec, "initial-pw"))
	assert.Equal(t, 2, fake.writeCount,
		"second reconcile must be a no-op — kubeaid-cli does not rotate user passwords")
}
