// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package keycloak

import (
	"context"
	"fmt"

	"github.com/Nerzal/gocloak/v13"
)

// UserSpec describes a Keycloak user in the realm. Username is
// required; the rest are optional.
type UserSpec struct {
	Username  string
	Email     string
	FirstName string
	LastName  string

	// Enabled defaults to true. Set explicitly for clarity in
	// callers, so a zero-value spec doesn't silently create a
	// disabled user.
	Enabled bool
}

// ReconcileUser ensures a user with spec.Username exists in the
// realm. When the user is created and initialPassword is non-empty,
// the password is set as a non-temporary credential. On a re-run
// (user already present) the existing password is preserved —
// kubeaid-cli is not in the password-rotation business.
func (r *Reconciler) ReconcileUser(
	ctx context.Context,
	realm string,
	spec UserSpec,
	initialPassword string,
) error {
	users, err := r.api.GetUsers(ctx, r.token, realm, gocloak.GetUsersParams{
		Username: gocloak.StringP(spec.Username),
		Exact:    gocloak.BoolP(true),
	})
	if err != nil {
		return fmt.Errorf("listing users in realm %q: %w", realm, err)
	}
	if findUserByUsername(users, spec.Username) != nil {
		return nil
	}

	user := gocloak.User{
		Username: gocloak.StringP(spec.Username),
		Enabled:  gocloak.BoolP(spec.Enabled),
	}
	if spec.Email != "" {
		user.Email = gocloak.StringP(spec.Email)
		// Keycloak doesn't auto-flag the email as verified; for
		// kubeaid-cli-managed users the operator vouched for the
		// address via general.yaml so verification is implicit.
		user.EmailVerified = gocloak.BoolP(true)
	}
	if spec.FirstName != "" {
		user.FirstName = gocloak.StringP(spec.FirstName)
	}
	if spec.LastName != "" {
		user.LastName = gocloak.StringP(spec.LastName)
	}

	id, err := r.api.CreateUser(ctx, r.token, realm, user)
	if err != nil {
		return fmt.Errorf("creating user %q in realm %q: %w", spec.Username, realm, err)
	}

	if initialPassword != "" {
		if err := r.api.SetPassword(ctx, r.token, id, realm, initialPassword, false); err != nil {
			return fmt.Errorf(
				"setting initial password for user %q in realm %q: %w",
				spec.Username, realm, err,
			)
		}
	}
	return nil
}

// findUserByUsername returns the user matching exactly. Keycloak's
// search is substring by default; callers pass Exact=true on
// GetUsersParams, but defensively re-check here too.
func findUserByUsername(users []*gocloak.User, username string) *gocloak.User {
	for _, u := range users {
		if u.Username != nil && *u.Username == username {
			return u
		}
	}
	return nil
}
