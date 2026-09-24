// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package keycloak

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

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
	api *gocloak.GoCloak

	// baseURL, adminUser and adminPassword are kept so the Reconciler
	// can log in again when the admin token expires. Keycloak's
	// master-realm admin-cli tokens live 60 s by default, far shorter
	// than a full SIEM reconcile run.
	baseURL       string
	adminUser     string
	adminPassword string

	// httpClient is used by the raw admin-API helper (rest.go) for the
	// endpoints gocloak does not cover (auth flows, scope mappings,
	// identity-provider mappers, ...).
	httpClient *http.Client

	// dryRun makes every Ensure* method report the change it would make
	// without issuing a write. Legacy Reconcile* methods ignore it.
	dryRun bool

	mu          sync.Mutex
	token       string
	tokenExpiry time.Time
}

// tokenRefreshMargin is how long before the access token's expiry the
// Reconciler proactively logs in again.
const tokenRefreshMargin = 10 * time.Second

// NewReconciler logs in as admin against Keycloak's master realm and
// returns a Reconciler holding the resulting access token. baseURL is
// Keycloak's HTTP root — kubeaid-cli's bootstrap passes the cluster's
// public https://<cluster.Keycloak.DNS>/auth (the keycloakx chart
// serves Keycloak under /auth).
func NewReconciler(ctx context.Context, baseURL, adminUser, adminPassword string) (*Reconciler, error) {
	return NewReconcilerWithHTTPClient(ctx, baseURL, adminUser, adminPassword, nil)
}

// NewReconcilerWithHTTPClient is NewReconciler with a caller-supplied
// HTTP client (custom CA, timeouts). A nil client means
// http.DefaultClient.
func NewReconcilerWithHTTPClient(
	ctx context.Context,
	baseURL, adminUser, adminPassword string,
	httpClient *http.Client,
) (*Reconciler, error) {
	api := gocloak.NewClient(baseURL)
	if httpClient == nil {
		httpClient = http.DefaultClient
	} else if httpClient.Transport != nil {
		api.RestyClient().SetTransport(httpClient.Transport)
	}
	r := &Reconciler{
		api:           api,
		baseURL:       baseURL,
		adminUser:     adminUser,
		adminPassword: adminPassword,
		httpClient:    httpClient,
	}
	if err := r.login(ctx); err != nil {
		return nil, err
	}
	return r, nil
}

// SetDryRun switches the Ensure* methods between reporting and
// applying. It does not affect the legacy Reconcile* methods.
func (r *Reconciler) SetDryRun(dryRun bool) { r.dryRun = dryRun }

// DryRun reports whether the Reconciler is in dry-run mode.
func (r *Reconciler) DryRun() bool { return r.dryRun }

// login fetches a fresh admin access token.
func (r *Reconciler) login(ctx context.Context) error {
	jwt, err := r.api.LoginAdmin(ctx, r.adminUser, r.adminPassword, adminLoginRealm)
	if err != nil {
		return fmt.Errorf("logging into Keycloak as admin: %w", err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.token = jwt.AccessToken
	if jwt.ExpiresIn > 0 {
		r.tokenExpiry = time.Now().Add(time.Duration(jwt.ExpiresIn) * time.Second)
	} else {
		r.tokenExpiry = time.Time{}
	}
	return nil
}

// tok returns an access token, logging in again first when the
// current one is about to expire. A failed refresh falls back to the
// stale token: the call then fails with 401 and is retried by the raw
// helper, or surfaces as an error to the caller.
func (r *Reconciler) tok(ctx context.Context) string {
	r.mu.Lock()
	expiry, token := r.tokenExpiry, r.token
	r.mu.Unlock()
	if !expiry.IsZero() && time.Until(expiry) < tokenRefreshMargin && r.adminPassword != "" {
		if err := r.login(ctx); err == nil {
			r.mu.Lock()
			token = r.token
			r.mu.Unlock()
		}
	}
	return token
}

// isNotFound reports whether err represents a 404 from Keycloak's
// admin API. gocloak wraps the HTTP response in *gocloak.APIError
// with the status code intact, so errors.As is the idiomatic test.
func isNotFound(err error) bool {
	var apiErr *gocloak.APIError
	return errors.As(err, &apiErr) && apiErr.Code == http.StatusNotFound
}
