// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package keycloak

import (
	"context"
	"fmt"

	"github.com/Nerzal/gocloak/v13"
)

// ClientSpec describes a Keycloak client (OIDC application) in a
// shape that's natural for callers; ReconcileClient maps it onto
// gocloak's pointer-everywhere Client representation.
type ClientSpec struct {
	// ClientID is the OIDC client_id (also Keycloak's display name
	// in the admin UI's Clients list). Required.
	ClientID string

	// PublicClient = true means PKCE / no client secret (browser
	// SSO, kubelogin). false means confidential — Keycloak issues
	// (or accepts a pre-set) client secret.
	PublicClient bool

	// ServiceAccountsEnabled = true makes Keycloak auto-create a
	// service-account user for this confidential client. Only
	// valid when PublicClient is false.
	ServiceAccountsEnabled bool

	// StandardFlowEnabled controls authorization-code (browser)
	// flow. DirectAccessGrantsEnabled controls Resource Owner
	// Password Credentials. Default both off; callers opt in.
	StandardFlowEnabled       bool
	DirectAccessGrantsEnabled bool

	// RedirectURIs / WebOrigins are required for browser flows
	// (PKCE). Ignored for purely backend confidential clients.
	RedirectURIs []string
	WebOrigins   []string

	// Secret pre-sets the confidential client's secret on creation.
	// When empty, Keycloak generates a random one. Ignored for
	// PublicClient = true.
	Secret string

	// DeviceAuthorizationGrantEnabled controls whether the client can
	// initiate the OAuth 2.0 Device Authorization Grant flow
	// (RFC 8628). Required for headless CLI tools — NetBird's
	// `netbird up`, for instance — that can't host an
	// authorization-code redirect URI. Keycloak stores this as the
	// `oauth2.device.authorization.grant.enabled` client attribute,
	// not a top-level Client field, so buildClient translates the
	// bool onto the Attributes map.
	DeviceAuthorizationGrantEnabled bool
}

// keycloakAttrDeviceAuthorizationGrantEnabled is the literal client-
// attribute key Keycloak reads to gate the OAuth 2.0 Device
// Authorization Grant flow (RFC 8628). Pulled out as a constant so
// the Reconciler's create and update paths set the same key — and
// so a grep for the attribute name lands on a single definition.
const keycloakAttrDeviceAuthorizationGrantEnabled = "oauth2.device.authorization.grant.enabled"

// ReconcileClient ensures a client matching spec.ClientID exists in
// the realm. For confidential clients the returned string is the
// effective client secret (either spec.Secret if pre-set on create,
// or the secret Keycloak generated and we read back). Public
// clients return "".
func (r *Reconciler) ReconcileClient(ctx context.Context, realm string, spec ClientSpec) (string, error) {
	clients, err := r.api.GetClients(ctx, r.token, realm, gocloak.GetClientsParams{
		ClientID: gocloak.StringP(spec.ClientID),
	})
	if err != nil {
		return "", fmt.Errorf("listing clients in realm %q: %w", realm, err)
	}

	if existing := findClientByClientID(clients, spec.ClientID); existing != nil {
		if spec.PublicClient {
			return "", nil
		}
		return r.fetchClientSecret(ctx, realm, *existing.ID)
	}

	created := buildClient(spec)
	id, err := r.api.CreateClient(ctx, r.token, realm, created)
	if err != nil {
		return "", fmt.Errorf("creating client %q in realm %q: %w", spec.ClientID, realm, err)
	}

	if spec.PublicClient {
		return "", nil
	}

	// For confidential clients with a pre-set secret, the secret
	// we POSTed is what Keycloak stored — return it verbatim
	// rather than a second round-trip.
	if spec.Secret != "" {
		return spec.Secret, nil
	}
	return r.fetchClientSecret(ctx, realm, id)
}

// findClientByClientID returns the client matching ClientID
// exactly. Keycloak's filter is a substring match, so a query for
// "netbird-client" can also surface "netbird-client-staging" — the
// caller's idempotency check needs an exact-string compare.
func findClientByClientID(clients []*gocloak.Client, clientID string) *gocloak.Client {
	for _, c := range clients {
		if c.ClientID != nil && *c.ClientID == clientID {
			return c
		}
	}
	return nil
}

// fetchClientSecret reads the credential for a confidential client.
// idOfClient is Keycloak's internal id (returned from CreateClient
// or in Client.ID), not the user-facing ClientID.
func (r *Reconciler) fetchClientSecret(ctx context.Context, realm, idOfClient string) (string, error) {
	cred, err := r.api.GetClientSecret(ctx, r.token, realm, idOfClient)
	if err != nil {
		return "", fmt.Errorf("reading secret for client %q: %w", idOfClient, err)
	}
	if cred == nil || cred.Value == nil {
		return "", fmt.Errorf("client %q has no credential value", idOfClient)
	}
	return *cred.Value, nil
}

func buildClient(spec ClientSpec) gocloak.Client {
	c := gocloak.Client{
		ClientID:                  gocloak.StringP(spec.ClientID),
		Enabled:                   gocloak.BoolP(true),
		Protocol:                  gocloak.StringP("openid-connect"),
		PublicClient:              gocloak.BoolP(spec.PublicClient),
		StandardFlowEnabled:       gocloak.BoolP(spec.StandardFlowEnabled),
		DirectAccessGrantsEnabled: gocloak.BoolP(spec.DirectAccessGrantsEnabled),
	}
	if !spec.PublicClient {
		c.ServiceAccountsEnabled = gocloak.BoolP(spec.ServiceAccountsEnabled)
		if spec.Secret != "" {
			c.Secret = gocloak.StringP(spec.Secret)
		}
	}
	if len(spec.RedirectURIs) > 0 {
		uris := append([]string(nil), spec.RedirectURIs...)
		c.RedirectURIs = &uris
	}
	if len(spec.WebOrigins) > 0 {
		origins := append([]string(nil), spec.WebOrigins...)
		c.WebOrigins = &origins
	}
	if spec.DeviceAuthorizationGrantEnabled {
		c.Attributes = &map[string]string{
			keycloakAttrDeviceAuthorizationGrantEnabled: "true",
		}
	}
	return c
}

// EnsureClientDeviceAuthorizationGrant sets/unsets the OAuth 2.0
// Device Authorization Grant (RFC 8628) capability on the named
// client. Idempotent — no-op when the attribute is already in the
// desired state.
//
// Keycloak stores this as the
// `oauth2.device.authorization.grant.enabled` client attribute;
// there's no top-level Client field for it, so ReconcileClient's
// create path uses the Attributes map and existing clusters
// retro-fit the attribute through this method.
//
// Called from ReconcileNetBird for the netbird-client OIDC client —
// NetBird's CLI (`netbird up`) uses this flow to authenticate from
// headless contexts. Without the attribute Keycloak rejects
// /protocol/openid-connect/auth/device with "Client is not allowed
// to initiate OAuth 2.0 Device Authorization Grant. The flow is
// disabled for the client."
func (r *Reconciler) EnsureClientDeviceAuthorizationGrant(
	ctx context.Context, realm, clientID string, enabled bool,
) error {
	clients, err := r.api.GetClients(ctx, r.token, realm, gocloak.GetClientsParams{
		ClientID: gocloak.StringP(clientID),
	})
	if err != nil {
		return fmt.Errorf("listing clients in realm %q: %w", realm, err)
	}
	client := findClientByClientID(clients, clientID)
	if client == nil {
		return fmt.Errorf("client %q not found in realm %q", clientID, realm)
	}

	want := "false"
	if enabled {
		want = "true"
	}

	attrs := map[string]string{}
	if client.Attributes != nil {
		attrs = *client.Attributes
	}
	if attrs[keycloakAttrDeviceAuthorizationGrantEnabled] == want {
		return nil
	}
	attrs[keycloakAttrDeviceAuthorizationGrantEnabled] = want

	// Send the existing client back with only Attributes changed, so
	// any other settings (operator customizations, fields not in
	// ClientSpec) are preserved on PUT. gocloak omits nil pointers
	// from the JSON body via `omitempty`, so Secret stays untouched
	// for confidential clients too.
	client.Attributes = &attrs
	if err := r.api.UpdateClient(ctx, r.token, realm, *client); err != nil {
		return fmt.Errorf("updating attributes on client %q: %w", clientID, err)
	}
	return nil
}
