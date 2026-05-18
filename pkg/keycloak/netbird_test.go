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

func TestReconcileNetBird_RequiredFields(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		spec NetBirdSpec
		want string
	}{
		{
			name: "missing realm",
			spec: NetBirdSpec{
				NetBirdMgmtURL:       "https://nb.acme.com",
				NetBirdBackendSecret: "s",
			},
			want: "spec.Realm",
		},
		{
			name: "missing mgmt URL",
			spec: NetBirdSpec{
				Realm:                "acme",
				NetBirdBackendSecret: "s",
			},
			want: "spec.NetBirdMgmtURL",
		},
		{
			name: "missing backend secret",
			spec: NetBirdSpec{
				Realm:          "acme",
				NetBirdMgmtURL: "https://nb.acme.com",
			},
			want: "spec.NetBirdBackendSecret",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r, _ := newTestReconciler(t)
			err := r.ReconcileNetBird(context.Background(), tc.spec)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestReconcileNetBird_HappyPathAndIdempotent(t *testing.T) {
	t.Parallel()

	r, fake := newTestReconciler(t)

	// realm-management is a Keycloak built-in that the real server
	// auto-creates per realm; the fake doesn't, so seed it here as
	// a plain confidential client (the fake's clientRolesFixture
	// supplies its view-users role keyed on this clientId).
	require.NoError(t, r.ReconcileRealm(context.Background(), "acme"))
	_, err := r.ReconcileClient(context.Background(), "acme", ClientSpec{
		ClientID:     realmManagementClientID,
		PublicClient: false,
	})
	require.NoError(t, err)
	beforeOrch := fake.writeCount

	spec := NetBirdSpec{
		Realm:                "acme",
		NetBirdMgmtURL:       "https://nb.acme.com",
		NetBirdBackendSecret: "pre-generated-secret-from-kubeaid-cli",
	}

	require.NoError(t, r.ReconcileNetBird(context.Background(), spec))
	firstRunWrites := fake.writeCount - beforeOrch

	require.NoError(t, r.ReconcileNetBird(context.Background(), spec))
	assert.Equal(t, firstRunWrites, fake.writeCount-beforeOrch,
		"second ReconcileNetBird must not write anything new")
}

// TestReconcileNetBird_AudienceMapperHasClientID guards against
// the audience-mismatch regression: NetBird Mgmt validates the JWT
// `aud` claim against IDP_CLIENT_ID (= netbird-client), so the api
// scope's audience mapper must insert exactly that — not the
// netbird-mgmt public URL the earlier revision used.
func TestReconcileNetBird_AudienceMapperHasClientID(t *testing.T) {
	t.Parallel()

	r, fake := newTestReconciler(t)
	require.NoError(t, r.ReconcileRealm(context.Background(), "acme"))
	_, err := r.ReconcileClient(context.Background(), "acme", ClientSpec{
		ClientID:     realmManagementClientID,
		PublicClient: false,
	})
	require.NoError(t, err)

	require.NoError(t, r.ReconcileNetBird(context.Background(), NetBirdSpec{
		Realm:                "acme",
		NetBirdMgmtURL:       "https://nb.acme.com",
		NetBirdBackendSecret: "s",
	}))

	apiScope := findScopeByName(t, fake, "acme", NetBirdAPIScopeName)
	mapper := findMapperByName(t, apiScope, netBirdAudienceMapperName)
	require.NotNil(t, mapper.ProtocolMappersConfig)
	require.NotNil(t, mapper.ProtocolMappersConfig.IncludedClientAudience)
	assert.Equal(t, netBirdClientID, *mapper.ProtocolMappersConfig.IncludedClientAudience,
		"audience must be the netbird-client client_id so JWT aud matches NetBird Mgmt's Audience config")
}

// TestReconcileProtocolMapper_DriftUpdate covers the retro-fit:
// a cluster that bootstrapped before the audience fix has a mapper
// whose included.client.audience is the public URL. Re-running
// ReconcileNetBird must overwrite that with netbird-client.
func TestReconcileProtocolMapper_DriftUpdate(t *testing.T) {
	t.Parallel()

	r, fake := newTestReconciler(t)
	seedRealm(t, r, fake)

	// Seed the api scope + an audience mapper pointing at the OLD
	// (URL-based) audience.
	require.NoError(t, r.ReconcileClientScope(context.Background(), testRealm, ClientScopeSpec{
		Name:     NetBirdAPIScopeName,
		Protocol: "openid-connect",
	}))
	require.NoError(t, r.ReconcileProtocolMapperOnClientScope(context.Background(), testRealm, NetBirdAPIScopeName, ProtocolMapperSpec{
		Name:           netBirdAudienceMapperName,
		Protocol:       "openid-connect",
		ProtocolMapper: "oidc-audience-mapper",
		Config: map[string]string{
			"included.client.audience": "https://stale.example.com",
		},
	}))
	preWrites := fake.writeCount

	// Now reconcile with the desired (fixed) config; must update.
	require.NoError(t, r.ReconcileProtocolMapperOnClientScope(context.Background(), testRealm, NetBirdAPIScopeName, ProtocolMapperSpec{
		Name:           netBirdAudienceMapperName,
		Protocol:       "openid-connect",
		ProtocolMapper: "oidc-audience-mapper",
		Config: map[string]string{
			"included.client.audience": netBirdClientID,
		},
	}))
	assert.Equal(t, preWrites+1, fake.writeCount, "drift triggers exactly one update")

	apiScope := findScopeByName(t, fake, testRealm, NetBirdAPIScopeName)
	mapper := findMapperByName(t, apiScope, netBirdAudienceMapperName)
	require.NotNil(t, mapper.ProtocolMappersConfig)
	require.NotNil(t, mapper.ProtocolMappersConfig.IncludedClientAudience)
	assert.Equal(t, netBirdClientID, *mapper.ProtocolMappersConfig.IncludedClientAudience)

	// Idempotent: same spec a second time is a no-op.
	require.NoError(t, r.ReconcileProtocolMapperOnClientScope(context.Background(), testRealm, NetBirdAPIScopeName, ProtocolMapperSpec{
		Name:           netBirdAudienceMapperName,
		Protocol:       "openid-connect",
		ProtocolMapper: "oidc-audience-mapper",
		Config: map[string]string{
			"included.client.audience": netBirdClientID,
		},
	}))
	assert.Equal(t, preWrites+1, fake.writeCount, "second reconcile is a no-op")
}

// findScopeByName / findMapperByName are tiny helpers for the
// audience-mapper assertions above. They keep the test bodies
// focused on what's interesting (the typed config field), not on
// the map-traversal mechanics.
func findScopeByName(t *testing.T, fake *fakeKeycloak, realm, name string) *gocloak.ClientScope {
	t.Helper()
	fake.mu.Lock()
	defer fake.mu.Unlock()
	for _, s := range fake.scopes[realm] {
		if s.Name != nil && *s.Name == name {
			return s
		}
	}
	t.Fatalf("scope %q not found in realm %q", name, realm)
	return nil
}

func findMapperByName(t *testing.T, scope *gocloak.ClientScope, name string) *gocloak.ProtocolMappers {
	t.Helper()
	if scope.ProtocolMappers == nil {
		t.Fatalf("scope %q has no mappers", *scope.Name)
	}
	for i := range *scope.ProtocolMappers {
		m := &(*scope.ProtocolMappers)[i]
		if m.Name != nil && *m.Name == name {
			return m
		}
	}
	t.Fatalf("mapper %q not found on scope %q", name, *scope.Name)
	return nil
}

func TestReconcileNetBird_BackendSecretIsHonored(t *testing.T) {
	t.Parallel()

	const preSet = "pre-generated-secret-from-kubeaid-cli"

	r, fake := newTestReconciler(t)
	require.NoError(t, r.ReconcileRealm(context.Background(), "acme"))
	_, err := r.ReconcileClient(context.Background(), "acme", ClientSpec{
		ClientID:     realmManagementClientID,
		PublicClient: false,
	})
	require.NoError(t, err)

	require.NoError(t, r.ReconcileNetBird(context.Background(), NetBirdSpec{
		Realm:                "acme",
		NetBirdMgmtURL:       "https://nb.acme.com",
		NetBirdBackendSecret: preSet,
	}))

	// Verify the netbird-backend client landed in the fake with
	// the pre-set secret (rather than a generated one).
	for _, c := range fake.clients["acme"] {
		if c.ClientID != nil && *c.ClientID == "netbird-backend" {
			require.NotNil(t, c.Secret)
			assert.Equal(t, preSet, *c.Secret)
			return
		}
	}
	t.Fatalf("netbird-backend client not found")
}
