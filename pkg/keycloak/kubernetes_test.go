// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package keycloak

import (
	"context"
	"testing"

	"github.com/Nerzal/gocloak/v13"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReconcileKubernetes_RequiredFields(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		spec KubernetesSpec
		want string
	}{
		{
			name: "missing realm",
			spec: KubernetesSpec{
				ClusterName: "acme-vpn",
			},
			want: "spec.Realm",
		},
		{
			name: "missing cluster name",
			spec: KubernetesSpec{
				Realm: "acme",
			},
			want: "spec.ClusterName",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r, _ := newTestReconciler(t)
			err := r.ReconcileKubernetes(context.Background(), tc.spec)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

// findClient returns the *gocloak.Client with the given ClientID from
// the fake's realm map, or nil if not found. Tiny helper that keeps
// the test bodies readable.
func findClient(fake *fakeKeycloak, realm, clientID string) *gocloak.Client {
	for _, c := range fake.clients[realm] {
		if c.ClientID != nil && *c.ClientID == clientID {
			return c
		}
	}
	return nil
}

// TestReconcileKubernetes_ClientCreatedWithExpectedShape verifies the
// `kubernetes-<ClusterName>` client lands with the public PKCE shape
// kubelogin expects + the default localhost redirects. Regression
// test for the refactor that moved this client out of
// ReconcileNetBird.
func TestReconcileKubernetes_ClientCreatedWithExpectedShape(t *testing.T) {
	t.Parallel()

	r, fake := newTestReconciler(t)
	require.NoError(t, r.ReconcileRealm(context.Background(), "acme"))

	require.NoError(t, r.ReconcileKubernetes(context.Background(), KubernetesSpec{
		Realm:       "acme",
		ClusterName: "acme-vpn",
	}))

	got := findClient(fake, "acme", "kubernetes-acme-vpn")
	require.NotNil(t, got, "expected kubernetes-acme-vpn client in realm 'acme'")

	require.NotNil(t, got.PublicClient)
	assert.True(t, *got.PublicClient, "should be a public PKCE client")
	require.NotNil(t, got.StandardFlowEnabled)
	assert.True(t, *got.StandardFlowEnabled, "standard auth-code flow required for kubelogin")

	require.NotNil(t, got.RedirectURIs)
	assert.ElementsMatch(t, defaultKubeloginRedirectURIs, *got.RedirectURIs,
		"default kubelogin redirects when none provided")
}

// TestReconcileKubernetes_CustomRedirectURIs lets operators override
// the default localhost callbacks (e.g., kubelogin running on custom
// ports or in a remote dev setup).
func TestReconcileKubernetes_CustomRedirectURIs(t *testing.T) {
	t.Parallel()

	r, fake := newTestReconciler(t)
	require.NoError(t, r.ReconcileRealm(context.Background(), "acme"))

	custom := []string{"http://localhost:30000/callback"}
	require.NoError(t, r.ReconcileKubernetes(context.Background(), KubernetesSpec{
		Realm:        "acme",
		ClusterName:  "acme-vpn",
		RedirectURIs: custom,
	}))

	got := findClient(fake, "acme", "kubernetes-acme-vpn")
	require.NotNil(t, got)
	require.NotNil(t, got.RedirectURIs)
	assert.ElementsMatch(t, custom, *got.RedirectURIs)
}

// TestReconcileKubernetes_AssignsDefaultScopes pins the audience-mapper
// inheritance: when the caller passes NetBirdAPIScopeName (the typical
// VPN-cluster pattern, since the kubernetes client lives in the same
// realm as NetBird), the scope is assigned as a default on the client
// so kubelogin tokens carry the NetBird audience claim.
func TestReconcileKubernetes_AssignsDefaultScopes(t *testing.T) {
	t.Parallel()

	r, fake := newTestReconciler(t)
	require.NoError(t, r.ReconcileRealm(context.Background(), "acme"))

	// The scope itself must exist before AssignClientDefaultScopes
	// can attach it — usually ReconcileNetBird sets this up.
	require.NoError(t, r.ReconcileClientScope(context.Background(), "acme", ClientScopeSpec{
		Name:     NetBirdAPIScopeName,
		Protocol: "openid-connect",
	}))

	require.NoError(t, r.ReconcileKubernetes(context.Background(), KubernetesSpec{
		Realm:         "acme",
		ClusterName:   "acme-vpn",
		DefaultScopes: []string{NetBirdAPIScopeName},
	}))

	got := findClient(fake, "acme", "kubernetes-acme-vpn")
	require.NotNil(t, got)
	require.NotNil(t, got.DefaultClientScopes)
	assert.Contains(t, *got.DefaultClientScopes, NetBirdAPIScopeName)
}

// TestReconcileKubernetes_Idempotent guards the upsert semantics —
// second call must not write anything new. Mirrors the analogous
// assertion in TestReconcileNetBird_HappyPathAndIdempotent.
func TestReconcileKubernetes_Idempotent(t *testing.T) {
	t.Parallel()

	r, fake := newTestReconciler(t)
	require.NoError(t, r.ReconcileRealm(context.Background(), "acme"))
	require.NoError(t, r.ReconcileClientScope(context.Background(), "acme", ClientScopeSpec{
		Name:     NetBirdAPIScopeName,
		Protocol: "openid-connect",
	}))

	spec := KubernetesSpec{
		Realm:         "acme",
		ClusterName:   "acme-vpn",
		DefaultScopes: []string{NetBirdAPIScopeName},
	}

	require.NoError(t, r.ReconcileKubernetes(context.Background(), spec))
	firstRun := fake.writeCount

	require.NoError(t, r.ReconcileKubernetes(context.Background(), spec))
	assert.Equal(t, firstRun, fake.writeCount,
		"second ReconcileKubernetes must not write anything new")
}
