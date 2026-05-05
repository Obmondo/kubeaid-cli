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

	// VPNClusterName is the VPN cluster's name (cluster.name). The
	// VPN cluster's own kube-apiserver authenticates against the
	// `kubernetes-<VPNClusterName>` OIDC client created in the same
	// realm.
	VPNClusterName string

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

	// KubernetesRedirectURIs are the redirect URIs for kubelogin's
	// localhost callback. Defaults applied when empty.
	KubernetesRedirectURIs []string
}

// netBirdAPIScopeName is the ClientScope created in the realm and
// assigned as a default scope on netbird-client + netbird-backend.
// It carries the audience ProtocolMapper that asserts the token
// audience matches NetBird Mgmt's expected `aud` claim.
const netBirdAPIScopeName = "api"

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
	if spec.VPNClusterName == "" {
		return fmt.Errorf("ReconcileNetBird: spec.VPNClusterName is required")
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
	kubernetesClientID := "kubernetes-" + spec.VPNClusterName

	if _, err := r.ReconcileClient(ctx, spec.Realm, ClientSpec{
		ClientID:            netBirdClientID,
		PublicClient:        true,
		StandardFlowEnabled: true,
		RedirectURIs:        []string{spec.NetBirdMgmtURL + "/*"},
		WebOrigins:          []string{spec.NetBirdMgmtURL},
	}); err != nil {
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

	kubeRedirects := spec.KubernetesRedirectURIs
	if len(kubeRedirects) == 0 {
		kubeRedirects = []string{
			"http://localhost:8000",
			"http://localhost:18000",
		}
	}
	if _, err := r.ReconcileClient(ctx, spec.Realm, ClientSpec{
		ClientID:            kubernetesClientID,
		PublicClient:        true,
		StandardFlowEnabled: true,
		RedirectURIs:        kubeRedirects,
	}); err != nil {
		return err
	}

	if err := r.ReconcileClientScope(ctx, spec.Realm, ClientScopeSpec{
		Name:        netBirdAPIScopeName,
		Protocol:    "openid-connect",
		Description: "NetBird Management API audience",
	}); err != nil {
		return err
	}

	if err := r.ReconcileProtocolMapperOnClientScope(ctx, spec.Realm, netBirdAPIScopeName, ProtocolMapperSpec{
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

	for _, clientID := range []string{netBirdClientID, netBirdBackendID, kubernetesClientID} {
		if err := r.AssignClientDefaultScopes(ctx, spec.Realm, clientID, []string{netBirdAPIScopeName}); err != nil {
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
