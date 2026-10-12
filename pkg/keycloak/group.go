// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package keycloak

import (
	"context"
	"fmt"
	"net/url"
)

// EnsureGroup makes sure a top-level group with this exact name
// exists. Members are never touched.
func (r *Reconciler) EnsureGroup(ctx context.Context, realm, name string) (Change, error) {
	id, err := r.findGroupID(ctx, realm, name)
	if err != nil {
		return ChangeNone, err
	}
	if id != "" {
		return ChangeNone, nil
	}
	if r.dryRun {
		return ChangeCreated, nil
	}
	if err := r.do(ctx, methodPost, realmPath(realm, "/groups"), map[string]any{keyName: name}, nil); err != nil {
		return ChangeNone, fmt.Errorf("creating group %q in realm %q: %w", name, realm, err)
	}
	return ChangeCreated, nil
}

// EnsureGroupRealmRoles makes sure the group grants every listed
// realm role. Mappings not in the list are kept.
func (r *Reconciler) EnsureGroupRealmRoles(ctx context.Context, realm, group string, roles []string) (Change, error) {
	if len(roles) == 0 {
		return ChangeNone, nil
	}
	id, err := r.findGroupID(ctx, realm, group)
	if err != nil {
		return ChangeNone, err
	}
	if id == "" {
		if r.dryRun {
			// The group would be created earlier in this run.
			return ChangeCreated, nil
		}
		return ChangeNone, fmt.Errorf("group %q not found in realm %q", group, realm)
	}
	path := realmPath(realm, "/groups/"+url.PathEscape(id)+"/role-mappings/realm")
	var cur []map[string]any
	if err := r.do(ctx, methodGet, path, nil, &cur); err != nil {
		return ChangeNone, fmt.Errorf("reading realm roles of group %q: %w", group, err)
	}
	have := make([]string, 0, len(cur))
	for _, m := range cur {
		have = append(have, asString(m[keyName]))
	}
	add := missing(have, roles)
	if len(add) == 0 {
		return ChangeNone, nil
	}
	if r.dryRun {
		return ChangeUpdated, nil
	}
	refs, err := r.realmRoleRefs(ctx, realm, add)
	if err != nil {
		return ChangeNone, err
	}
	if err := r.do(ctx, methodPost, path, refs, nil); err != nil {
		return ChangeNone, fmt.Errorf("mapping realm roles %v to group %q: %w", add, group, err)
	}
	return ChangeUpdated, nil
}

// findGroupID returns the id of the top-level group named exactly
// name, or "" when there is none. Keycloak's search is a substring
// match over the whole tree, so the result is filtered.
func (r *Reconciler) findGroupID(ctx context.Context, realm, name string) (string, error) {
	q := url.Values{}
	q.Set("search", name)
	q.Set("briefRepresentation", "true")
	q.Set("max", "1000")
	var groups []map[string]any
	if err := r.do(ctx, methodGet, realmPath(realm, "/groups?"+q.Encode()), nil, &groups); err != nil {
		return "", fmt.Errorf("searching group %q in realm %q: %w", name, realm, err)
	}
	for _, g := range groups {
		if asString(g[keyName]) == name {
			return asString(g["id"]), nil
		}
	}
	return "", nil
}
