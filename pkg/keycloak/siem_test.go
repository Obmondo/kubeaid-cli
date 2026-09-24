// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package keycloak

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// allNone asserts every result reports no change.
func allNone(t *testing.T, results []Result) {
	t.Helper()
	for _, res := range results {
		assert.Equal(t, ChangeNone, res.Change, "%s %s should be ok (%s)", res.Kind, res.Name, res.Detail)
	}
}

const (
	testTenantName  = "Tenant 001"
	testClientID    = "dashboard"
	testBrowserFlow = "browser"
	testAnalystRole = "analyst"
	testGroup       = "tenant-001"
	testClaimName   = "claim.name"
)

func boolP(b bool) *bool { return &b }

func TestEnsureRealmSettings(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r, fake := newTestReconciler(t)
	seedRealm(t, r, fake)

	spec := RealmSettings{
		BruteForceProtected: boolP(true),
		OTPPolicy:           &OTPPolicy{Type: "totp", Algorithm: "HmacSHA256", Digits: 6, Period: 30},
	}

	r.SetDryRun(true)
	change, detail, err := r.EnsureRealmSettings(ctx, testRealm, spec)
	require.NoError(t, err)
	assert.Equal(t, ChangeUpdated, change)
	assert.Equal(t, "bruteForceProtected,otpPolicyAlgorithm", detail)
	c, err := r.EnsureRequiredAction(ctx, testRealm, "CONFIGURE_TOTP", true, true)
	require.NoError(t, err)
	assert.Equal(t, ChangeUpdated, c)
	assert.Equal(t, 0, fake.writeCount, "dry run must not write")

	r.SetDryRun(false)
	change, _, err = r.EnsureRealmSettings(ctx, testRealm, spec)
	require.NoError(t, err)
	assert.Equal(t, ChangeUpdated, change)
	_, err = r.EnsureRequiredAction(ctx, testRealm, "CONFIGURE_TOTP", true, true)
	require.NoError(t, err)
	assert.Equal(t, 2, fake.writeCount)
	assert.Equal(t, true, fake.realmReps[testRealm]["bruteForceProtected"])
	assert.Equal(t, true, fake.requiredActions[testRealm]["CONFIGURE_TOTP"]["defaultAction"])

	change, _, err = r.EnsureRealmSettings(ctx, testRealm, spec)
	require.NoError(t, err)
	assert.Equal(t, ChangeNone, change)
	c, err = r.EnsureRequiredAction(ctx, testRealm, "CONFIGURE_TOTP", true, true)
	require.NoError(t, err)
	assert.Equal(t, ChangeNone, c)
	assert.Equal(t, 2, fake.writeCount, "second run must not write")
}

func TestEnsureRealm(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r, fake := newTestReconciler(t)

	r.SetDryRun(true)
	c, err := r.EnsureRealm(ctx, "fresh")
	require.NoError(t, err)
	assert.Equal(t, ChangeCreated, c)
	assert.Equal(t, 0, fake.writeCount)

	r.SetDryRun(false)
	c, err = r.EnsureRealm(ctx, "fresh")
	require.NoError(t, err)
	assert.Equal(t, ChangeCreated, c)
	c, err = r.EnsureRealm(ctx, "fresh")
	require.NoError(t, err)
	assert.Equal(t, ChangeNone, c)
	assert.Equal(t, 1, fake.writeCount)
}

func TestEnsureRolesGroupsAndMappings(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r, fake := newTestReconciler(t)
	seedRealm(t, r, fake)

	run := func() []Change {
		var out []Change
		for _, role := range []RealmRoleSpec{{Name: testAnalystRole}, {Name: testGroup, Description: testTenantName}} {
			c, err := r.EnsureRealmRole(ctx, testRealm, role)
			require.NoError(t, err)
			out = append(out, c)
		}
		for _, g := range []string{testGroup, "analysts"} {
			c, err := r.EnsureGroup(ctx, testRealm, g)
			require.NoError(t, err)
			out = append(out, c)
		}
		c, err := r.EnsureGroupRealmRoles(ctx, testRealm, testGroup, []string{testGroup})
		require.NoError(t, err)
		return append(out, c)
	}

	r.SetDryRun(true)
	changes := run()
	assert.Equal(t, []Change{ChangeCreated, ChangeCreated, ChangeCreated, ChangeCreated, ChangeCreated}, changes)
	assert.Equal(t, 0, fake.writeCount, "dry run must not write")

	r.SetDryRun(false)
	changes = run()
	assert.Equal(t, []Change{ChangeCreated, ChangeCreated, ChangeCreated, ChangeCreated, ChangeUpdated}, changes)
	writes := fake.writeCount

	assert.Equal(t, []Change{ChangeNone, ChangeNone, ChangeNone, ChangeNone, ChangeNone}, run())
	assert.Equal(t, writes, fake.writeCount, "second run must not write")

	// Description drift is corrected; a group whose name merely
	// contains another group's name is not mistaken for it.
	fake.realmRoles[testRealm][testGroup]["description"] = "changed"
	c, err := r.EnsureRealmRole(ctx, testRealm, RealmRoleSpec{Name: testGroup, Description: testTenantName})
	require.NoError(t, err)
	assert.Equal(t, ChangeUpdated, c)
	c, err = r.EnsureGroup(ctx, testRealm, "tenant-00")
	require.NoError(t, err)
	assert.Equal(t, ChangeCreated, c)
}

func siemClientSpec() ManagedClientSpec {
	return ManagedClientSpec{
		ClientID:               testClientID,
		Name:                   "Dashboard",
		StandardFlowEnabled:    true,
		ServiceAccountsEnabled: true,
		FullScopeAllowed:       boolP(false),
		RootURL:                "https://dashboard.example.com",
		RedirectURIs:           []string{"https://dashboard.example.com/login"},
		WebOrigins:             []string{"https://dashboard.example.com"},
		PostLogoutRedirectURIs: []string{"https://dashboard.example.com/*"},
		Attributes:             map[string]string{"pkce.code.challenge.method": "S256"},
		Secret:                 "s3cret",
		DefaultClientScopes:    []string{"email"},
		ProtocolMappers: []ProtocolMapperSpec{{
			Name:           "realm roles",
			ProtocolMapper: "oidc-usermodel-realm-role-mapper",
			Config:         map[string]string{testClaimName: "roles", "multivalued": valueTrue, "introspection.token.claim": valueTrue},
		}},
		ScopeMappingsRealm:        []string{testAnalystRole},
		ScopeMappingsClient:       map[string][]string{realmManagementClientID: {"query-users"}},
		ServiceAccountClientRoles: map[string][]string{realmManagementClientID: {"view-users"}},
	}
}

func seedClientPrereqs(t *testing.T, r *Reconciler, fake *fakeKeycloak) {
	t.Helper()
	ctx := context.Background()
	_, err := r.ReconcileClient(ctx, testRealm, ClientSpec{ClientID: realmManagementClientID})
	require.NoError(t, err)
	require.NoError(t, r.ReconcileClientScope(ctx, testRealm, ClientScopeSpec{Name: "email", Protocol: protocolOIDC}))
	_, err = r.EnsureRealmRole(ctx, testRealm, RealmRoleSpec{Name: testAnalystRole})
	require.NoError(t, err)
	fake.writeCount = 0
}

func TestEnsureClient_DryRunCreateIdempotent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r, fake := newTestReconciler(t)
	seedRealm(t, r, fake)
	seedClientPrereqs(t, r, fake)
	spec := siemClientSpec()

	r.SetDryRun(true)
	results, err := r.EnsureClient(ctx, testRealm, spec)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, ChangeCreated, results[0].Change)
	assert.Equal(t, 0, fake.writeCount, "dry run must not write")

	r.SetDryRun(false)
	results, err = r.EnsureClient(ctx, testRealm, spec)
	require.NoError(t, err)
	assert.Len(t, results, 6)
	assert.Positive(t, fake.writeCount)

	writes := fake.writeCount
	results, err = r.EnsureClient(ctx, testRealm, spec)
	require.NoError(t, err)
	allNone(t, results)
	assert.Equal(t, writes, fake.writeCount, "second run must not write")

	r.SetDryRun(true)
	results, err = r.EnsureClient(ctx, testRealm, spec)
	require.NoError(t, err)
	allNone(t, results)
}

func TestEnsureClient_DriftSupersetsAndSecret(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r, fake := newTestReconciler(t)
	seedRealm(t, r, fake)
	seedClientPrereqs(t, r, fake)
	spec := siemClientSpec()
	_, err := r.EnsureClient(ctx, testRealm, spec)
	require.NoError(t, err)

	// An operator added an extra redirect URI and changed the name;
	// the secret was rotated in Keycloak only.
	for _, c := range fake.clients[testRealm] {
		if derefString(c.ClientID) == testClientID {
			extra := []string{"https://extra.example.com/cb"}
			c.RedirectURIs = &extra
			c.Name = strP("Renamed")
			c.Secret = strP("rotated")
		}
	}
	fake.writeCount = 0

	r.SetDryRun(true)
	results, err := r.EnsureClient(ctx, testRealm, spec)
	require.NoError(t, err)
	assert.Equal(t, ChangeUpdated, results[0].Change)
	assert.Equal(t, "name,redirectUris+=https://dashboard.example.com/login", results[0].Detail)
	assert.Equal(t, KindClientSecret, results[1].Kind)
	assert.Equal(t, ChangeUpdated, results[1].Change)
	assert.Equal(t, 0, fake.writeCount)

	r.SetDryRun(false)
	_, err = r.EnsureClient(ctx, testRealm, spec)
	require.NoError(t, err)
	for _, c := range fake.clients[testRealm] {
		if derefString(c.ClientID) == testClientID {
			assert.Equal(t, []string{"https://extra.example.com/cb", "https://dashboard.example.com/login"}, *c.RedirectURIs,
				"missing URIs are added, extra ones kept")
			assert.Equal(t, "Dashboard", derefString(c.Name))
			assert.Equal(t, "s3cret", derefString(c.Secret))
		}
	}

	// Mapper config drift is corrected in place.
	for _, m := range fake.clientMappers[testRealm] {
		asMap(m[0]["config"])[testClaimName] = "wrong"
	}
	results, err = r.EnsureClient(ctx, testRealm, spec)
	require.NoError(t, err)
	assert.Equal(t, KindClientMapper, results[2].Kind)
	assert.Equal(t, ChangeUpdated, results[2].Change)
}

func strP(s string) *string { return &s }

func TestEnsureBrowserMFAFlow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r, fake := newTestReconciler(t)
	seedRealm(t, r, fake)
	spec := MFAFlowSpec{Alias: "browser-mfa", CopyFrom: testBrowserFlow, Bind: true}

	r.SetDryRun(true)
	results, err := r.EnsureBrowserMFAFlow(ctx, testRealm, spec)
	require.NoError(t, err)
	assert.Equal(t, 0, fake.writeCount, "dry run must not write")
	kinds := map[string]Change{}
	for _, res := range results {
		kinds[res.Kind] = merge2(kinds[res.Kind], res.Change)
	}
	assert.Equal(t, ChangeCreated, kinds[KindAuthFlow])
	assert.Equal(t, ChangeUpdated, kinds[KindAuthExecution])
	assert.Equal(t, ChangeNone, kinds[KindAuthFlowOrder])
	assert.Equal(t, ChangeUpdated, kinds[KindRealmBrowserFlow])

	r.SetDryRun(false)
	_, err = r.EnsureBrowserMFAFlow(ctx, testRealm, spec)
	require.NoError(t, err)
	assert.Equal(t, "browser-mfa", fake.realmReps[testRealm]["browserFlow"])
	execs := fake.flows[testRealm]["browser-mfa"]
	byProvider := map[string]string{}
	for _, e := range execs {
		byProvider[asString(e["providerId"])+"|"+asString(e["displayName"])] = asString(e["requirement"])
	}
	assert.Equal(t, "REQUIRED", byProvider[providerOTPForm+"|OTP Form"])
	assert.Equal(t, "DISABLED", byProvider["conditional-user-configured|Condition - user configured"])
	assert.Equal(t, "REQUIRED", byProvider["|browser-mfa Browser - Conditional 2FA"])
	assert.Equal(t, "ALTERNATIVE", fake.flows[testRealm][testBrowserFlow][7]["requirement"], "the source flow is untouched")

	writes := fake.writeCount
	results, err = r.EnsureBrowserMFAFlow(ctx, testRealm, spec)
	require.NoError(t, err)
	allNone(t, results)
	assert.Equal(t, writes, fake.writeCount, "second run must not write")
}

func merge2(a, b Change) Change {
	if b > a {
		return b
	}
	return a
}

func TestEnsureBrowserMFAFlow_FixesOrderBeforeBinding(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r, fake := newTestReconciler(t)
	seedRealm(t, r, fake)

	// A copy whose OTP sub-flow sits above the password form: the
	// state that broke every browser login.
	broken := stockBrowserFlow(fake.nextID)
	pw := broken[4]
	broken = append(broken[:4:4], broken[5:]...)
	broken = append(broken, pw)
	fake.flows[testRealm]["browser-mfa"] = broken
	fake.writeCount = 0
	spec := MFAFlowSpec{Alias: "browser-mfa", CopyFrom: testBrowserFlow, Bind: true}

	r.SetDryRun(true)
	results, err := r.EnsureBrowserMFAFlow(ctx, testRealm, spec)
	require.NoError(t, err)
	assert.Equal(t, 0, fake.writeCount)
	var order Change
	for _, res := range results {
		if res.Kind == KindAuthFlowOrder {
			order = res.Change
		}
	}
	assert.Equal(t, ChangeUpdated, order)

	r.SetDryRun(false)
	_, err = r.EnsureBrowserMFAFlow(ctx, testRealm, spec)
	require.NoError(t, err)
	got := fake.flows[testRealm]["browser-mfa"]
	assert.Equal(t, providerUsernamePassword, got[4]["providerId"], "password form is first in forms")
	assert.Equal(t, "browser-mfa", fake.realmReps[testRealm]["browserFlow"])
}

func TestEnsureBrowserMFAFlow_NeverBindsBrokenFlow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r, fake := newTestReconciler(t)
	seedRealm(t, r, fake)
	flow := stockBrowserFlow(fake.nextID)
	fake.flows[testRealm]["no-pw"] = append(flow[:4:4], flow[5:]...)

	_, err := r.EnsureBrowserMFAFlow(ctx, testRealm, MFAFlowSpec{Alias: "no-pw", CopyFrom: testBrowserFlow, Bind: true})
	require.Error(t, err)
	assert.Equal(t, testBrowserFlow, fake.realmReps[testRealm]["browserFlow"])
}

func TestEnsureIdentityProvider(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r, fake := newTestReconciler(t)
	seedRealm(t, r, fake)
	spec := IdentityProviderSpec{
		Alias:        "tenant-001-idp",
		DisplayName:  testTenantName,
		ProviderID:   "oidc",
		Enabled:      true,
		Config:       map[string]string{"issuer": "https://idp.example.com", "clientId": "kc"},
		ClientSecret: "hidden",
		Group:        testGroup,
	}

	r.SetDryRun(true)
	results, err := r.EnsureIdentityProvider(ctx, testRealm, spec)
	require.NoError(t, err)
	assert.Len(t, results, 2)
	assert.Equal(t, 0, fake.writeCount)

	r.SetDryRun(false)
	_, err = r.EnsureIdentityProvider(ctx, testRealm, spec)
	require.NoError(t, err)
	writes := fake.writeCount
	results, err = r.EnsureIdentityProvider(ctx, testRealm, spec)
	require.NoError(t, err)
	allNone(t, results)
	assert.Equal(t, writes, fake.writeCount, "masked secret must not count as drift")
	assert.Equal(t, "/"+testGroup, asMap(fake.idpMappers[testRealm]["tenant-001-idp"][0]["config"])["group"])

	spec.Config["issuer"] = "https://new.example.com"
	results, err = r.EnsureIdentityProvider(ctx, testRealm, spec)
	require.NoError(t, err)
	assert.Equal(t, ChangeUpdated, results[0].Change)
	assert.Equal(t, "config.issuer", results[0].Detail)
}

func TestRawCallRelogsInOn401(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r, fake := newTestReconciler(t)
	seedRealm(t, r, fake)
	logins := fake.logins

	fake.rejectNext = 1
	c, err := r.EnsureGroup(ctx, testRealm, "g")
	require.NoError(t, err)
	assert.Equal(t, ChangeCreated, c)
	assert.Equal(t, logins+1, fake.logins, "a 401 triggers exactly one re-login")

	fake.rejectNext = 5
	_, err = r.EnsureGroup(ctx, testRealm, "g")
	require.Error(t, err, "a second 401 after re-login is returned")
}

func TestChangeString(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "ok", ChangeNone.String())
	assert.Equal(t, "create", ChangeCreated.String())
	assert.Equal(t, "update", ChangeUpdated.String())
	assert.Equal(t, "unknown", Change(9).String())
}
