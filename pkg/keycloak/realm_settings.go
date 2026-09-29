// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package keycloak

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// OTPPolicy is the realm's one-time-password policy.
type OTPPolicy struct {
	Type      string // "totp" or "hotp"
	Algorithm string // "HmacSHA1", "HmacSHA256", ...
	Digits    int
	Period    int
}

// RealmSettings lists the realm attributes the SIEM reconciler
// manages. Nil / zero fields are left alone.
type RealmSettings struct {
	BruteForceProtected *bool
	OTPPolicy           *OTPPolicy
}

// EnsureRealm makes sure the realm exists (enabled). Dry-run aware
// counterpart of ReconcileRealm.
func (r *Reconciler) EnsureRealm(ctx context.Context, realm string) (Change, error) {
	err := r.do(ctx, methodGet, realmPath(realm, ""), nil, &map[string]any{})
	if err == nil {
		return ChangeNone, nil
	}
	if !isRawNotFound(err) {
		return ChangeNone, fmt.Errorf("looking up realm %q: %w", realm, err)
	}
	if r.dryRun {
		return ChangeCreated, nil
	}
	body := map[string]any{keyRealm: realm, keyEnabled: true}
	if err := r.do(ctx, methodPost, "/admin/realms", body, nil); err != nil {
		return ChangeNone, fmt.Errorf("creating realm %q: %w", realm, err)
	}
	return ChangeCreated, nil
}

// EnsureRealmSettings brings the managed realm attributes in line and
// reports which ones drifted. Keycloak's realm PUT merges the fields
// present in the body, so only the drifted fields are sent.
func (r *Reconciler) EnsureRealmSettings(ctx context.Context, realm string, spec RealmSettings) (Change, string, error) {
	var cur map[string]any
	if err := r.do(ctx, methodGet, realmPath(realm, ""), nil, &cur); err != nil {
		return ChangeNone, "", fmt.Errorf("reading realm %q: %w", realm, err)
	}
	patch := map[string]any{}
	if spec.BruteForceProtected != nil && asBool(cur["bruteForceProtected"]) != *spec.BruteForceProtected {
		patch["bruteForceProtected"] = *spec.BruteForceProtected
	}
	if p := spec.OTPPolicy; p != nil {
		if p.Type != "" && asString(cur["otpPolicyType"]) != p.Type {
			patch["otpPolicyType"] = p.Type
		}
		if p.Algorithm != "" && asString(cur["otpPolicyAlgorithm"]) != p.Algorithm {
			patch["otpPolicyAlgorithm"] = p.Algorithm
		}
		if p.Digits != 0 && asInt(cur["otpPolicyDigits"]) != p.Digits {
			patch["otpPolicyDigits"] = p.Digits
		}
		if p.Period != 0 && asInt(cur["otpPolicyPeriod"]) != p.Period {
			patch["otpPolicyPeriod"] = p.Period
		}
	}
	if len(patch) == 0 {
		return ChangeNone, "", nil
	}
	detail := strings.Join(sortedKeys(patch), ",")
	if r.dryRun {
		return ChangeUpdated, detail, nil
	}
	if err := r.do(ctx, methodPut, realmPath(realm, ""), patch, nil); err != nil {
		return ChangeNone, "", fmt.Errorf("updating realm %q (%s): %w", realm, detail, err)
	}
	return ChangeUpdated, detail, nil
}

// EnsureRequiredAction sets enabled / defaultAction on a realm
// required action (e.g. CONFIGURE_TOTP). defaultAction stamps the
// action on every newly created user.
func (r *Reconciler) EnsureRequiredAction(ctx context.Context, realm, alias string, enabled, defaultAction bool) (Change, error) {
	path := realmPath(realm, "/authentication/required-actions/"+url.PathEscape(alias))
	var cur map[string]any
	if err := r.do(ctx, methodGet, path, nil, &cur); err != nil {
		return ChangeNone, fmt.Errorf("reading required action %q in realm %q: %w", alias, realm, err)
	}
	if asBool(cur[keyEnabled]) == enabled && asBool(cur["defaultAction"]) == defaultAction {
		return ChangeNone, nil
	}
	if r.dryRun {
		return ChangeUpdated, nil
	}
	cur[keyEnabled] = enabled
	cur["defaultAction"] = defaultAction
	if err := r.do(ctx, methodPut, path, cur, nil); err != nil {
		return ChangeNone, fmt.Errorf("updating required action %q in realm %q: %w", alias, realm, err)
	}
	return ChangeUpdated, nil
}

// --- loose JSON helpers ---

func asBool(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return t == valueTrue
	}
	return false
}

func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

func asInt(v any) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	}
	return 0
}

func asStrings(v any) []string {
	list, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func asMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// missing returns the entries of want that are not in have, in
// want's order.
func missing(have, want []string) []string {
	set := make(map[string]bool, len(have))
	for _, h := range have {
		set[h] = true
	}
	var out []string
	for _, w := range want {
		if !set[w] {
			out = append(out, w)
			set[w] = true
		}
	}
	return out
}
