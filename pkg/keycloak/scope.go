// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package keycloak

import (
	"context"
	"fmt"

	"github.com/Nerzal/gocloak/v13"
)

// ClientScopeSpec describes a Keycloak client scope. Only Name and
// Protocol are required; description is optional.
type ClientScopeSpec struct {
	Name        string
	Protocol    string // typically "openid-connect"
	Description string

	// IncludeInTokenScope controls whether the scope's name appears
	// in the issued token's `scope` claim. NetBird's docs require
	// this on for the `groups` scope so the operator's Keycloak
	// groups land in the JWT — Keycloak stores it as the
	// `include.in.token.scope` attribute on the ClientScope
	// representation, not a top-level field.
	IncludeInTokenScope bool
}

// ReconcileClientScope ensures a client scope with spec.Name exists
// in the realm. Idempotent: if a scope with the same name already
// exists the function no-ops.
func (r *Reconciler) ReconcileClientScope(ctx context.Context, realm string, spec ClientScopeSpec) error {
	scopes, err := r.api.GetClientScopes(ctx, r.token, realm)
	if err != nil {
		return fmt.Errorf("listing client scopes in realm %q: %w", realm, err)
	}

	if findClientScopeByName(scopes, spec.Name) != nil {
		return nil
	}

	scope := gocloak.ClientScope{
		Name:     gocloak.StringP(spec.Name),
		Protocol: gocloak.StringP(spec.Protocol),
	}
	if spec.Description != "" {
		scope.Description = gocloak.StringP(spec.Description)
	}
	if spec.IncludeInTokenScope {
		scope.ClientScopeAttributes = &gocloak.ClientScopeAttributes{
			IncludeInTokenScope: gocloak.StringP("true"),
		}
	}

	if _, err := r.api.CreateClientScope(ctx, r.token, realm, scope); err != nil {
		return fmt.Errorf("creating client scope %q in realm %q: %w", spec.Name, realm, err)
	}
	return nil
}

// AssignClientDefaultScopes ensures every name in scopeNames is set
// as a default scope on the client identified by clientID (the
// user-facing OIDC client_id). Existing assignments are preserved;
// missing ones are added. Unknown scope names return an error
// rather than silently being ignored.
func (r *Reconciler) AssignClientDefaultScopes(ctx context.Context, realm, clientID string, scopeNames []string) error {
	clients, err := r.api.GetClients(ctx, r.token, realm, gocloak.GetClientsParams{
		ClientID: gocloak.StringP(clientID),
	})
	if err != nil {
		return fmt.Errorf("listing clients in realm %q: %w", realm, err)
	}
	target := findClientByClientID(clients, clientID)
	if target == nil || target.ID == nil {
		return fmt.Errorf("client %q not found in realm %q", clientID, realm)
	}

	scopes, err := r.api.GetClientScopes(ctx, r.token, realm)
	if err != nil {
		return fmt.Errorf("listing client scopes in realm %q: %w", realm, err)
	}

	already := stringSet(target.DefaultClientScopes)
	for _, name := range scopeNames {
		if already[name] {
			continue
		}
		scope := findClientScopeByName(scopes, name)
		if scope == nil || scope.ID == nil {
			return fmt.Errorf("client scope %q not found in realm %q", name, realm)
		}
		if err := r.api.AddDefaultScopeToClient(ctx, r.token, realm, *target.ID, *scope.ID); err != nil {
			return fmt.Errorf(
				"assigning scope %q to client %q in realm %q: %w",
				name, clientID, realm, err,
			)
		}
	}
	return nil
}

func findClientScopeByName(scopes []*gocloak.ClientScope, name string) *gocloak.ClientScope {
	for _, s := range scopes {
		if s.Name != nil && *s.Name == name {
			return s
		}
	}
	return nil
}

// stringSet builds a lookup set from an optional *[]string. Nil and
// empty slices both yield an empty (non-nil) map, simplifying
// callers' membership checks.
func stringSet(p *[]string) map[string]bool {
	out := make(map[string]bool)
	if p == nil {
		return out
	}
	for _, s := range *p {
		out[s] = true
	}
	return out
}
