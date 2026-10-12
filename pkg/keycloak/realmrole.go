// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package keycloak

import (
	"context"
	"fmt"
	"net/url"
)

// RealmRoleSpec describes a realm role. Description is only managed
// when non-empty.
type RealmRoleSpec struct {
	Name        string
	Description string
}

// EnsureRealmRole creates the realm role if missing and updates its
// description when the spec sets one that differs.
func (r *Reconciler) EnsureRealmRole(ctx context.Context, realm string, spec RealmRoleSpec) (Change, error) {
	cur, err := r.getRealmRole(ctx, realm, spec.Name)
	if err != nil {
		return ChangeNone, err
	}
	if cur == nil {
		if r.dryRun {
			return ChangeCreated, nil
		}
		body := map[string]any{keyName: spec.Name}
		if spec.Description != "" {
			body["description"] = spec.Description
		}
		if err := r.do(ctx, methodPost, realmPath(realm, "/roles"), body, nil); err != nil {
			return ChangeNone, fmt.Errorf("creating realm role %q in realm %q: %w", spec.Name, realm, err)
		}
		return ChangeCreated, nil
	}
	if spec.Description == "" || asString(cur["description"]) == spec.Description {
		return ChangeNone, nil
	}
	if r.dryRun {
		return ChangeUpdated, nil
	}
	cur["description"] = spec.Description
	if err := r.do(ctx, methodPut, realmPath(realm, "/roles/"+url.PathEscape(spec.Name)), cur, nil); err != nil {
		return ChangeNone, fmt.Errorf("updating realm role %q in realm %q: %w", spec.Name, realm, err)
	}
	return ChangeUpdated, nil
}

// getRealmRole returns the role representation, or nil when absent.
func (r *Reconciler) getRealmRole(ctx context.Context, realm, name string) (map[string]any, error) {
	var role map[string]any
	err := r.do(ctx, methodGet, realmPath(realm, "/roles/"+url.PathEscape(name)), nil, &role)
	if isRawNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading realm role %q in realm %q: %w", name, realm, err)
	}
	return role, nil
}

// roleRefs resolves role names to the {id, name} representations the
// role-mapping endpoints expect. In dry-run mode a role that does not
// exist yet (it would be created earlier in the same run) resolves to
// a placeholder instead of failing.
func (r *Reconciler) realmRoleRefs(ctx context.Context, realm string, names []string) ([]map[string]any, error) {
	out := make([]map[string]any, 0, len(names))
	for _, n := range names {
		role, err := r.getRealmRole(ctx, realm, n)
		if err != nil {
			return nil, err
		}
		if role == nil {
			if r.dryRun {
				out = append(out, map[string]any{keyName: n})
				continue
			}
			return nil, fmt.Errorf("realm role %q not found in realm %q", n, realm)
		}
		out = append(out, map[string]any{"id": role["id"], keyName: role[keyName]})
	}
	return out, nil
}
