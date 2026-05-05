// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package keycloak

import (
	"context"
	"fmt"

	"github.com/Nerzal/gocloak/v13"
)

// ReconcileRealm ensures a realm with the given name exists. The
// realm is created enabled (Realm.Enabled = true) — Keycloak's
// default leaves it disabled, which would silently break OIDC
// discovery and token issuance.
func (r *Reconciler) ReconcileRealm(ctx context.Context, name string) error {
	_, err := r.api.GetRealm(ctx, r.token, name)
	if err == nil {
		return nil
	}
	if !isNotFound(err) {
		return fmt.Errorf("looking up realm %q: %w", name, err)
	}

	if _, err := r.api.CreateRealm(ctx, r.token, gocloak.RealmRepresentation{
		Realm:   gocloak.StringP(name),
		Enabled: gocloak.BoolP(true),
	}); err != nil {
		return fmt.Errorf("creating realm %q: %w", name, err)
	}
	return nil
}
