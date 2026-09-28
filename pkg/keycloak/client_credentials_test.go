// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package keycloak

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// clientCredentialsServer answers the client-credentials grant of one
// client in testRealm and passes everything else to the fake.
func clientCredentialsServer(t *testing.T, fake *fakeKeycloak, clientID, secret string) (*httptest.Server, *int) {
	t.Helper()
	logins := 0
	inner := fake.handler()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/realms/"+testRealm+"/protocol/openid-connect/token" {
			inner.ServeHTTP(w, r)
			return
		}
		_ = r.ParseForm()
		// gocloak sends the client credentials as HTTP basic auth.
		user, pass, _ := r.BasicAuth()
		if r.PostForm.Get("grant_type") != "client_credentials" || user != clientID || pass != secret {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized_client"}`))
			return
		}
		logins++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "fake-client-token", "expires_in": 300, "token_type": "Bearer",
		})
	}))
	t.Cleanup(srv.Close)
	return srv, &logins
}

func TestClientCredentialsLogin(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fake := newFakeKeycloak()
	srv, logins := clientCredentialsServer(t, fake, "siem-reconciler", "s3cret")

	// The realm exists: the client lives in it.
	admin, err := NewReconciler(ctx, srv.URL, "admin", "p")
	require.NoError(t, err)
	_, err = admin.EnsureRealm(ctx, testRealm)
	require.NoError(t, err)
	adminLogins := fake.logins

	r, err := NewReconcilerWithClientCredentials(ctx, srv.URL, testRealm, "siem-reconciler", "s3cret", nil)
	require.NoError(t, err)
	assert.Equal(t, 1, *logins)
	assert.Equal(t, adminLogins, fake.logins, "no master-realm admin login")

	c, err := r.EnsureRealmRole(ctx, testRealm, RealmRoleSpec{Name: "analyst"})
	require.NoError(t, err)
	assert.Equal(t, ChangeCreated, c)

	// An expired token is renewed with the client secret.
	fake.mu.Lock()
	fake.rejectNext = 1
	fake.mu.Unlock()
	c, err = r.EnsureRealmRole(ctx, testRealm, RealmRoleSpec{Name: "analyst"})
	require.NoError(t, err)
	assert.Equal(t, ChangeNone, c)
	assert.Equal(t, 2, *logins)
}

func TestClientCredentialsLoginRejected(t *testing.T) {
	t.Parallel()
	fake := newFakeKeycloak()
	srv, _ := clientCredentialsServer(t, fake, "siem-reconciler", "s3cret")
	_, err := NewReconcilerWithClientCredentials(context.Background(), srv.URL, testRealm, "siem-reconciler", "wrong", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "client siem-reconciler")
}
