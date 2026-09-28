// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package reconcile

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/Obmondo/kubeaid-cli/pkg/siem/config"
	"github.com/Obmondo/kubeaid-cli/pkg/siem/report"
	"github.com/Obmondo/kubeaid-cli/pkg/siem/secrets"
)

const (
	adminWho    = "admin"
	adminPwdKey = "KEYCLOAK_PASSWORD"
)

// tokenServer issues tokens for the master admin (password "p") and for
// the client siem-reconciler in realm soc when clientExists.
func tokenServer(t *testing.T, clientExists bool) (*httptest.Server, map[string]int) {
	t.Helper()
	logins := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		user, pass, _ := r.BasicAuth()
		var who string
		switch {
		case r.URL.Path == "/realms/master/protocol/openid-connect/token" && r.PostForm.Get("password") == "p":
			who = adminWho
		case r.URL.Path == "/realms/soc/protocol/openid-connect/token" && clientExists &&
			user == "siem-reconciler" && pass == "cs":
			who = "client"
		default:
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized_client"}`))
			return
		}
		logins[who]++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": who, "expires_in": 300, "token_type": "Bearer"})
	}))
	t.Cleanup(srv.Close)
	return srv, logins
}

func loginConfig(url string, admin bool) config.Keycloak {
	k := config.Keycloak{
		URL: url, Realm: "soc", AdminUsername: "admin",
		ClientCredentials: &config.ClientCredentials{
			ClientID:  "siem-reconciler",
			SecretRef: config.SecretRef{Namespace: "security-operations", Name: "siem-reconciler-keycloak", Key: "KEYCLOAK_CLIENT_SECRET"},
		},
	}
	if admin {
		k.AdminSecretRef = config.SecretRef{Namespace: "keycloakx", Name: "keycloak-admin", Key: adminPwdKey}
	}
	return k
}

func loginStore() secrets.Store {
	return secrets.Store{Kube: fake.NewClientset(
		secret("security-operations", "siem-reconciler-keycloak", map[string]string{"KEYCLOAK_CLIENT_SECRET": "cs"}),
		secret("keycloakx", "keycloak-admin", map[string]string{adminPwdKey: "p"}),
	)}
}

func TestKeycloakLoginPrefersClient(t *testing.T) {
	t.Parallel()
	srv, logins := tokenServer(t, true)
	kc, res := keycloakLogin(context.Background(), loginConfig(srv.URL, true), loginStore(), srv.Client())
	require.NotNil(t, kc)
	assert.Equal(t, report.ActionOK, res.Action)
	assert.Equal(t, "client siem-reconciler", res.Detail)
	assert.Equal(t, map[string]int{"client": 1}, logins, "the admin password is not used")
}

func TestKeycloakLoginFallsBackToAdminUntilClientExists(t *testing.T) {
	t.Parallel()
	srv, logins := tokenServer(t, false)
	kc, res := keycloakLogin(context.Background(), loginConfig(srv.URL, true), loginStore(), srv.Client())
	require.NotNil(t, kc)
	assert.Equal(t, report.ActionSkip, res.Action)
	assert.Contains(t, res.Detail, "master admin admin (fallback: client siem-reconciler")
	assert.Equal(t, map[string]int{adminWho: 1}, logins)
}

func TestKeycloakLoginClientOnly(t *testing.T) {
	t.Parallel()
	srv, logins := tokenServer(t, false)
	kc, res := keycloakLogin(context.Background(), loginConfig(srv.URL, false), loginStore(), srv.Client())
	assert.Nil(t, kc)
	assert.Equal(t, report.ActionError, res.Action)
	assert.Empty(t, logins, "no admin fallback without adminSecretRef")

	srv, _ = tokenServer(t, true)
	kc, res = keycloakLogin(context.Background(), loginConfig(srv.URL, false), loginStore(), srv.Client())
	require.NotNil(t, kc)
	assert.Equal(t, report.ActionOK, res.Action)
}

func TestKeycloakLoginAdminOnly(t *testing.T) {
	t.Parallel()
	srv, _ := tokenServer(t, true)
	k := loginConfig(srv.URL, true)
	k.ClientCredentials = nil
	kc, res := keycloakLogin(context.Background(), k, loginStore(), srv.Client())
	require.NotNil(t, kc)
	assert.Equal(t, report.ActionOK, res.Action)
	assert.Equal(t, "master admin admin", res.Detail)
}
