// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package keycloak

import (
	"context"
	"fmt"

	"github.com/Nerzal/gocloak/v13"
)

// AssignClientServiceAccountRole grants the named role from
// targetClientID's role catalogue to srcClientID's auto-created
// service-account user. Both client IDs are user-facing
// (clientId), not Keycloak's internal id.
//
// Idempotent: when the service-account user already holds the
// role, no API write is performed.
//
// Typical use: granting `view-users` from the built-in
// `realm-management` client to `netbird-backend`'s service-account
// so NetBird can resolve user identities through Keycloak.
func (r *Reconciler) AssignClientServiceAccountRole(
	ctx context.Context,
	realm, srcClientID, targetClientID, roleName string,
) error {
	srcID, err := r.lookupClientInternalID(ctx, realm, srcClientID)
	if err != nil {
		return err
	}
	targetID, err := r.lookupClientInternalID(ctx, realm, targetClientID)
	if err != nil {
		return err
	}

	svcAccount, err := r.api.GetClientServiceAccount(ctx, r.token, realm, srcID)
	if err != nil {
		return fmt.Errorf(
			"reading service-account user for client %q in realm %q: %w",
			srcClientID, realm, err,
		)
	}
	if svcAccount == nil || svcAccount.ID == nil {
		return fmt.Errorf(
			"client %q in realm %q has no service-account user (serviceAccountsEnabled?)",
			srcClientID, realm,
		)
	}

	existing, err := r.api.GetClientRolesByUserID(ctx, r.token, realm, targetID, *svcAccount.ID)
	if err != nil {
		return fmt.Errorf(
			"reading existing service-account roles in realm %q: %w", realm, err,
		)
	}
	for _, role := range existing {
		if role.Name != nil && *role.Name == roleName {
			return nil
		}
	}

	role, err := r.api.GetClientRole(ctx, r.token, realm, targetID, roleName)
	if err != nil {
		return fmt.Errorf(
			"looking up role %q on client %q in realm %q: %w",
			roleName, targetClientID, realm, err,
		)
	}
	if role == nil {
		return fmt.Errorf(
			"role %q not found on client %q in realm %q",
			roleName, targetClientID, realm,
		)
	}

	if err := r.api.AddClientRolesToUser(
		ctx, r.token, realm, targetID, *svcAccount.ID, []gocloak.Role{*role},
	); err != nil {
		return fmt.Errorf(
			"granting role %q from client %q to service-account of %q in realm %q: %w",
			roleName, targetClientID, srcClientID, realm, err,
		)
	}
	return nil
}

// lookupClientInternalID resolves a user-facing OIDC client_id to
// Keycloak's internal id (the UUID admin APIs require for further
// lookups). Returns a wrapped error if not found.
func (r *Reconciler) lookupClientInternalID(ctx context.Context, realm, clientID string) (string, error) {
	clients, err := r.api.GetClients(ctx, r.token, realm, gocloak.GetClientsParams{
		ClientID: gocloak.StringP(clientID),
	})
	if err != nil {
		return "", fmt.Errorf("listing clients in realm %q: %w", realm, err)
	}
	c := findClientByClientID(clients, clientID)
	if c == nil || c.ID == nil {
		return "", fmt.Errorf("client %q not found in realm %q", clientID, realm)
	}
	return *c.ID, nil
}
