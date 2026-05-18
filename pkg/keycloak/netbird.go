// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package keycloak

import (
	"context"
	"fmt"
)

// NetBirdSpec describes the realm-side resources NetBird needs in
// the customer's Keycloak realm. Populated from cluster.keycloak.*
// + the pre-generated netbird-backend secret kubeaid-cli also
// templates into the SealedSecret consumed by NetBird Mgmt.
type NetBirdSpec struct {
	// Realm is the Keycloak realm name (typically derived from the
	// public DNS — see cluster.keycloak.realm).
	Realm string

	// NetBirdMgmtURL is the public URL of the NetBird Management
	// UI (e.g., https://netbird.<vpn-dns>). Used as the redirect
	// URI for `netbird-client` (browser SSO) and as the audience
	// for the netbird-backend's tokens.
	NetBirdMgmtURL string

	// NetBirdBackendSecret is the pre-generated client secret to
	// assign to `netbird-backend` on create. Pre-setting avoids a
	// second git push: kubeaid-cli renders the SealedSecret with
	// this value before ever calling the Keycloak admin API, then
	// hands the same value to ReconcileClient via spec.Secret.
	NetBirdBackendSecret string
}

// NetBirdAPIScopeName is the ClientScope created in the realm and
// assigned as a default scope on netbird-client + netbird-backend.
// It carries the audience ProtocolMapper that asserts the token
// audience matches NetBird Mgmt's expected `aud` claim.
//
// Exported because the kubernetes client created by
// ReconcileKubernetes inherits this scope (so kubelogin tokens share
// the NetBird audience mapping) — callers pass this in
// KubernetesSpec.DefaultScopes.
const NetBirdAPIScopeName = "api"

// netBirdAudienceMapperName is the human-readable name of the
// audience ProtocolMapper attached to the api ClientScope.
const netBirdAudienceMapperName = "Audience for NetBird Management API"

// realmManagementClientID is Keycloak's built-in client whose
// roles control admin-API access. Granting `view-users` from this
// client to netbird-backend's service account is what lets NetBird
// Mgmt resolve user identities through Keycloak.
const realmManagementClientID = "realm-management"

// netBirdViewUsersRole is the role granted to netbird-backend's
// service account on the realm-management client.
const netBirdViewUsersRole = "view-users"

// ReconcileNetBird creates everything NetBird (and the VPN
// cluster's own kube-apiserver) needs in the customer's Keycloak
// realm. Idempotent end-to-end: every step delegates to a
// Reconcile* / Assign* helper that no-ops when the resource
// already exists.
//
// Order matters — clients depend on the realm; the api scope must
// exist before it can be assigned to clients; the audience mapper
// must be on the scope before kube-apiserver tokens get the
// expected `aud` claim; the service-account role grant requires
// netbird-backend to exist (so its service-account user has been
// auto-created).
func (r *Reconciler) ReconcileNetBird(ctx context.Context, spec NetBirdSpec) error {
	if spec.Realm == "" {
		return fmt.Errorf("ReconcileNetBird: spec.Realm is required")
	}
	if spec.NetBirdMgmtURL == "" {
		return fmt.Errorf("ReconcileNetBird: spec.NetBirdMgmtURL is required")
	}
	if spec.NetBirdBackendSecret == "" {
		return fmt.Errorf("ReconcileNetBird: spec.NetBirdBackendSecret is required")
	}

	if err := r.ReconcileRealm(ctx, spec.Realm); err != nil {
		return err
	}

	netBirdClientID := "netbird-client"
	netBirdBackendID := "netbird-backend"

	if _, err := r.ReconcileClient(ctx, spec.Realm, ClientSpec{
		ClientID:                        netBirdClientID,
		PublicClient:                    true,
		StandardFlowEnabled:             true,
		DeviceAuthorizationGrantEnabled: true,
		RedirectURIs:                    []string{spec.NetBirdMgmtURL + "/*"},
		WebOrigins:                      []string{spec.NetBirdMgmtURL},
	}); err != nil {
		return err
	}
	// Idempotent retro-fit for clusters where netbird-client already
	// exists from a pre-device-flow bootstrap: ReconcileClient is
	// create-if-missing only, so the Attributes baked into the new
	// ClientSpec wouldn't reach the live client without this update.
	// NetBird's CLI (`netbird up`) relies on the device-authorization
	// flow — see EnsureClientDeviceAuthorizationGrant's doc comment.
	if err := r.EnsureClientDeviceAuthorizationGrant(ctx, spec.Realm, netBirdClientID, true); err != nil {
		return err
	}

	if _, err := r.ReconcileClient(ctx, spec.Realm, ClientSpec{
		ClientID:                  netBirdBackendID,
		PublicClient:              false,
		ServiceAccountsEnabled:    true,
		StandardFlowEnabled:       false,
		DirectAccessGrantsEnabled: false,
		Secret:                    spec.NetBirdBackendSecret,
	}); err != nil {
		return err
	}

	if err := r.ReconcileClientScope(ctx, spec.Realm, ClientScopeSpec{
		Name:        NetBirdAPIScopeName,
		Protocol:    "openid-connect",
		Description: "NetBird Management API audience",
	}); err != nil {
		return err
	}

	if err := r.ReconcileProtocolMapperOnClientScope(ctx, spec.Realm, NetBirdAPIScopeName, ProtocolMapperSpec{
		Name:           netBirdAudienceMapperName,
		Protocol:       "openid-connect",
		ProtocolMapper: "oidc-audience-mapper",
		Config: map[string]string{
			"included.client.audience": spec.NetBirdMgmtURL,
			"id.token.claim":           "true",
			"access.token.claim":       "true",
		},
	}); err != nil {
		return err
	}

	for _, clientID := range []string{netBirdClientID, netBirdBackendID} {
		if err := r.AssignClientDefaultScopes(ctx, spec.Realm, clientID, []string{NetBirdAPIScopeName}); err != nil {
			return err
		}
	}

	if err := r.AssignClientServiceAccountRole(
		ctx, spec.Realm,
		netBirdBackendID, realmManagementClientID, netBirdViewUsersRole,
	); err != nil {
		return err
	}

	return nil
}
