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

// TestReconcileNetBird_GroupsScopeAndMapper guards the JWT Group
// Sync wiring NetBird's Dashboard toggle ("Settings → Groups →
// Enable JWT Group Sync") expects to find pre-baked in Keycloak:
//
//   - a `groups` ClientScope with `include.in.token.scope` = true
//   - a Group Membership ProtocolMapper on it (claim.name = groups,
//     full.path = false, on for ID/access/userinfo tokens)
//   - the scope attached as a default on netbird-client
//
// Per NetBird's self-hosted Keycloak guide (Steps 1–3); kubeaid-cli
// owns these so the operator only has to flip the Dashboard toggle.
func TestReconcileNetBird_GroupsScopeAndMapper(t *testing.T) {
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

	// Scope exists with the token-scope attribute on.
	groupsScope := findScopeByName(t, fake, NetBirdGroupsScopeName)
	require.NotNil(t, groupsScope.ClientScopeAttributes,
		"groups scope must carry the include.in.token.scope attribute")
	require.NotNil(t, groupsScope.ClientScopeAttributes.IncludeInTokenScope)
	assert.Equal(t, "true", *groupsScope.ClientScopeAttributes.IncludeInTokenScope)

	// Mapper config follows NetBird's docs Step 2.
	mapper := findMapperByName(t, groupsScope, netBirdGroupsMapperName)
	require.NotNil(t, mapper.ProtocolMapper)
	assert.Equal(t, "oidc-group-membership-mapper", *mapper.ProtocolMapper)
	require.NotNil(t, mapper.ProtocolMappersConfig)
	cfg := mapper.ProtocolMappersConfig
	require.NotNil(t, cfg.ClaimName)
	assert.Equal(t, "groups", *cfg.ClaimName)
	require.NotNil(t, cfg.FullPath)
	assert.Equal(t, "false", *cfg.FullPath)
	for name, ptr := range map[string]*string{
		"id.token.claim":       cfg.IDTokenClaim,
		"access.token.claim":   cfg.AccessTokenClaim,
		"userinfo.token.claim": cfg.UserinfoTokenClaim,
	} {
		require.NotNilf(t, ptr, "%s must be set", name)
		assert.Equalf(t, "true", *ptr, "%s must be true", name)
	}

	// Attached to netbird-client as a default scope.
	nbClient := findClientInFake(t, fake, netBirdClientID)
	require.NotNil(t, nbClient.DefaultClientScopes)
	assert.Contains(t, *nbClient.DefaultClientScopes, NetBirdGroupsScopeName,
		"groups must be a default scope on netbird-client")

	// netbird-backend doesn't issue user tokens → must NOT inherit
	// the scope (matches the docstring + NetBird's docs Step 3).
	nbBackend := findClientInFake(t, fake, "netbird-backend")
	if nbBackend.DefaultClientScopes != nil {
		assert.NotContains(t, *nbBackend.DefaultClientScopes, NetBirdGroupsScopeName,
			"groups scope must not bleed onto the backend client")
	}
}

// TestReconcileNetBird_ScopesHaveConsentScreenText asserts the
// human-friendly display labels Keycloak shows on the
// "Grant Access to netbird-client" consent screen — "API",
// "Groups" — instead of the raw scope names ("api", "groups")
// which clash visually with the built-in scopes Keycloak already
// title-cases ("Email address", "User profile", "User roles").
func TestReconcileNetBird_ScopesHaveConsentScreenText(t *testing.T) {
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

	for scopeName, wantLabel := range map[string]string{
		NetBirdAPIScopeName:    "API",
		NetBirdGroupsScopeName: "Groups",
	} {
		scope := findScopeByName(t, fake, scopeName)
		require.NotNilf(t, scope.ClientScopeAttributes,
			"%s scope must carry attributes", scopeName)
		require.NotNilf(t, scope.ClientScopeAttributes.ConsentScreenText,
			"%s scope is missing consent.screen.text — operator sees raw %q on the consent page",
			scopeName, scopeName)
		assert.Equal(t, wantLabel, *scope.ClientScopeAttributes.ConsentScreenText)
	}
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

	apiScope := findScopeByName(t, fake, NetBirdAPIScopeName)
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

	apiScope := findScopeByName(t, fake, NetBirdAPIScopeName)
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
func findScopeByName(t *testing.T, fake *fakeKeycloak, name string) *gocloak.ClientScope {
	t.Helper()
	fake.mu.Lock()
	defer fake.mu.Unlock()
	for _, s := range fake.scopes[testRealm] {
		if s.Name != nil && *s.Name == name {
			return s
		}
	}
	t.Fatalf("scope %q not found in realm %q", name, testRealm)
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
