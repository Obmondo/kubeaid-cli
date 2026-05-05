// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package keycloak

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/Nerzal/gocloak/v13"
)

// adminLoginRealm is the realm against which kubeaid-cli authenticates
// for admin operations. Keycloak's `master` realm always exists and
// holds the admin user.
const adminLoginRealm = "master"

// Reconciler issues idempotent admin-API calls against a running
// Keycloak. Construct via NewReconciler with admin credentials; each
// Reconcile* method either creates the resource if missing or
// no-ops when it already exists.
type Reconciler struct {
	api   *gocloak.GoCloak
	token string
}

// NewReconciler logs in as admin against Keycloak's master realm and
// returns a Reconciler holding the resulting access token. baseURL
// is Keycloak's HTTP root (e.g. http://localhost:8080 when reaching
// it through a port-forward to the keycloakx Service).
func NewReconciler(ctx context.Context, baseURL, adminUser, adminPassword string) (*Reconciler, error) {
	api := gocloak.NewClient(baseURL)
	jwt, err := api.LoginAdmin(ctx, adminUser, adminPassword, adminLoginRealm)
	if err != nil {
		return nil, fmt.Errorf("logging into Keycloak as admin: %w", err)
	}
	return &Reconciler{api: api, token: jwt.AccessToken}, nil
}

// isNotFound reports whether err represents a 404 from Keycloak's
// admin API. gocloak wraps the HTTP response in *gocloak.APIError
// with the status code intact, so errors.As is the idiomatic test.
func isNotFound(err error) bool {
	var apiErr *gocloak.APIError
	return errors.As(err, &apiErr) && apiErr.Code == http.StatusNotFound
}
