// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package keycloak

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// Result kinds for the authentication flow.
const (
	KindAuthFlow         = "auth-flow"
	KindAuthExecution    = "auth-execution"
	KindAuthFlowOrder    = "auth-flow-order"
	KindRealmBrowserFlow = "realm-browser-flow"
)

// Keycloak authenticator provider ids and requirements used below.
const (
	providerOTPForm          = "auth-otp-form"
	providerUsernamePassword = "auth-username-password-form"
	conditionalPrefix        = "conditional-"

	requirementRequired = "REQUIRED"
	requirementDisabled = "DISABLED"
)

// maxRaisePriority bounds the raise-priority loop; the stock forms
// sub-flow has two or three steps.
const maxRaisePriority = 8

// MFAFlowSpec describes the browser flow that makes OTP mandatory.
type MFAFlowSpec struct {
	// Alias of the managed flow, e.g. "browser-mfa".
	Alias string
	// CopyFrom is the flow copied when Alias does not exist yet;
	// normally the built-in "browser" (built-in flows are read-only).
	CopyFrom string
	// Bind makes Alias the realm's browser flow, but only after the
	// flow has been verified.
	Bind bool
}

// execution is the subset of Keycloak's
// AuthenticationExecutionInfoRepresentation the flow logic reads. The
// raw map is kept so PUTs send the full representation back
// (including priority: a PUT without it moves the step to the top).
type execution struct {
	raw map[string]any
}

func (e execution) id() string          { return asString(e.raw["id"]) }
func (e execution) level() int          { return asInt(e.raw["level"]) }
func (e execution) provider() string    { return asString(e.raw["providerId"]) }
func (e execution) requirement() string { return asString(e.raw["requirement"]) }

func (e execution) name() string {
	if n := asString(e.raw["displayName"]); n != "" {
		return n
	}
	return e.provider()
}

// EnsureBrowserMFAFlow makes OTP mandatory for every browser login:
//
//  1. copy CopyFrom to Alias when Alias does not exist;
//  2. in Alias, the sub-flow holding "OTP Form" becomes REQUIRED, its
//     condition steps DISABLED and "OTP Form" REQUIRED;
//  3. "Username Password Form" is raised to be the first step of its
//     sub-flow (the OTP step must never run before the user is known);
//  4. the result is re-read and verified; only a verified flow is
//     bound as the realm's browser flow.
//
// In dry-run mode the same steps run against an in-memory copy, so the
// report shows exactly what an apply would do.
func (r *Reconciler) EnsureBrowserMFAFlow(ctx context.Context, realm string, spec MFAFlowSpec) ([]Result, error) {
	if spec.Alias == "" || spec.CopyFrom == "" {
		return nil, errors.New("MFA flow needs an alias and a flow to copy from")
	}
	var results []Result
	add := func(kind, name string, c Change) {
		results = append(results, Result{Kind: kind, Name: name, Change: c})
	}

	exists, err := r.flowExists(ctx, realm, spec.Alias)
	if err != nil {
		return nil, err
	}
	var execs []execution
	switch {
	case exists:
		add(KindAuthFlow, spec.Alias, ChangeNone)
		execs, err = r.flowExecutions(ctx, realm, spec.Alias)
	case r.dryRun:
		add(KindAuthFlow, spec.Alias, ChangeCreated)
		// A copy has the same steps as its source.
		execs, err = r.flowExecutions(ctx, realm, spec.CopyFrom)
	default:
		path := realmPath(realm, "/authentication/flows/"+url.PathEscape(spec.CopyFrom)+"/copy")
		if err := r.do(ctx, methodPost, path, map[string]any{"newName": spec.Alias}, nil); err != nil {
			return nil, fmt.Errorf("copying flow %q to %q: %w", spec.CopyFrom, spec.Alias, err)
		}
		add(KindAuthFlow, spec.Alias, ChangeCreated)
		execs, err = r.flowExecutions(ctx, realm, spec.Alias)
	}
	if err != nil {
		return results, err
	}

	// Step 2: requirements.
	fixes, err := planOTPRequirements(execs)
	if err != nil {
		return results, err
	}
	for _, f := range fixes {
		add(KindAuthExecution, spec.Alias+"/"+execs[f.index].name()+"="+f.requirement, ChangeUpdated)
		if r.dryRun {
			execs[f.index].raw["requirement"] = f.requirement
			continue
		}
		body := copyMap(execs[f.index].raw)
		body["requirement"] = f.requirement
		path := realmPath(realm, "/authentication/flows/"+url.PathEscape(spec.Alias)+"/executions")
		if err := r.do(ctx, methodPut, path, body, nil); err != nil {
			return results, fmt.Errorf("setting %q to %s: %w", execs[f.index].name(), f.requirement, err)
		}
	}
	if len(fixes) > 0 && !r.dryRun {
		if execs, err = r.flowExecutions(ctx, realm, spec.Alias); err != nil {
			return results, err
		}
	}

	// Step 3: order.
	execs, orderChange, err := r.ensurePasswordFormFirst(ctx, realm, spec.Alias, execs)
	if err != nil {
		return results, err
	}
	add(KindAuthFlowOrder, spec.Alias+"/"+providerUsernamePassword+"-first", orderChange)

	// Step 4: verify, then bind.
	if err := verifyMFAFlow(execs); err != nil {
		return results, fmt.Errorf("flow %q failed verification, not binding it: %w", spec.Alias, err)
	}
	if !spec.Bind {
		return results, nil
	}
	change, err := r.bindBrowserFlow(ctx, realm, spec.Alias)
	if err != nil {
		return results, err
	}
	add(KindRealmBrowserFlow, spec.Alias, change)
	return results, nil
}

func (r *Reconciler) flowExists(ctx context.Context, realm, alias string) (bool, error) {
	var flows []map[string]any
	if err := r.do(ctx, methodGet, realmPath(realm, "/authentication/flows"), nil, &flows); err != nil {
		return false, fmt.Errorf("listing authentication flows in realm %q: %w", realm, err)
	}
	for _, f := range flows {
		if asString(f["alias"]) == alias {
			return true, nil
		}
	}
	return false, nil
}

func (r *Reconciler) flowExecutions(ctx context.Context, realm, alias string) ([]execution, error) {
	var raw []map[string]any
	path := realmPath(realm, "/authentication/flows/"+url.PathEscape(alias)+"/executions")
	if err := r.do(ctx, methodGet, path, nil, &raw); err != nil {
		return nil, fmt.Errorf("reading executions of flow %q: %w", alias, err)
	}
	out := make([]execution, len(raw))
	for i, m := range raw {
		out[i] = execution{raw: m}
	}
	return out, nil
}

type requirementFix struct {
	index       int
	requirement string
}

// planOTPRequirements lists the requirement changes that make the OTP
// step unconditional.
func planOTPRequirements(execs []execution) ([]requirementFix, error) {
	otp := indexOfProvider(execs, providerOTPForm)
	if otp < 0 {
		return nil, fmt.Errorf("no %s step in the flow", providerOTPForm)
	}
	parent := parentOf(execs, otp)
	if parent < 0 {
		return nil, errors.New("the OTP Form is not inside a sub-flow")
	}
	var fixes []requirementFix
	if execs[parent].requirement() != requirementRequired {
		fixes = append(fixes, requirementFix{index: parent, requirement: requirementRequired})
	}
	for _, c := range childrenOf(execs, parent) {
		switch {
		case execs[c].provider() == providerOTPForm && execs[c].requirement() != requirementRequired:
			fixes = append(fixes, requirementFix{index: c, requirement: requirementRequired})
		case strings.HasPrefix(execs[c].provider(), conditionalPrefix) && execs[c].requirement() != requirementDisabled:
			fixes = append(fixes, requirementFix{index: c, requirement: requirementDisabled})
		}
	}
	return fixes, nil
}

// ensurePasswordFormFirst raises "Username Password Form" until it is
// the first step of its sub-flow.
func (r *Reconciler) ensurePasswordFormFirst(ctx context.Context, realm, alias string, execs []execution) ([]execution, Change, error) {
	pw := indexOfProvider(execs, providerUsernamePassword)
	if pw < 0 {
		return execs, ChangeNone, fmt.Errorf("no %s step in flow %q", providerUsernamePassword, alias)
	}
	if isFirstChild(execs, pw) {
		return execs, ChangeNone, nil
	}
	if r.dryRun {
		return moveToFirstChild(execs, pw), ChangeUpdated, nil
	}
	id := execs[pw].id()
	for range maxRaisePriority {
		path := realmPath(realm, "/authentication/executions/"+url.PathEscape(id)+"/raise-priority")
		if err := r.do(ctx, methodPost, path, nil, nil); err != nil {
			return execs, ChangeNone, fmt.Errorf("raising %s: %w", providerUsernamePassword, err)
		}
		var err error
		if execs, err = r.flowExecutions(ctx, realm, alias); err != nil {
			return execs, ChangeNone, err
		}
		if pw = indexOfProvider(execs, providerUsernamePassword); pw >= 0 && isFirstChild(execs, pw) {
			return execs, ChangeUpdated, nil
		}
	}
	return execs, ChangeNone, fmt.Errorf("%s is still not first in its sub-flow after %d raises", providerUsernamePassword, maxRaisePriority)
}

// verifyMFAFlow checks the invariants a working MFA browser flow needs.
func verifyMFAFlow(execs []execution) error {
	pw := indexOfProvider(execs, providerUsernamePassword)
	otp := indexOfProvider(execs, providerOTPForm)
	switch {
	case pw < 0:
		return errors.New("no Username Password Form")
	case otp < 0:
		return errors.New("no OTP Form")
	case !isFirstChild(execs, pw):
		return errors.New("the Username Password Form is not the first step of its sub-flow")
	case execs[pw].requirement() != requirementRequired:
		return errors.New("the Username Password Form is not REQUIRED")
	case execs[otp].requirement() != requirementRequired:
		return errors.New("the OTP Form is not REQUIRED")
	}
	otpParent := parentOf(execs, otp)
	if otpParent < 0 || execs[otpParent].requirement() != requirementRequired {
		return errors.New("the sub-flow holding OTP Form is not REQUIRED")
	}
	if otpParent < pw {
		return errors.New("the OTP sub-flow runs before Username Password Form")
	}
	for _, c := range childrenOf(execs, otpParent) {
		if strings.HasPrefix(execs[c].provider(), conditionalPrefix) && execs[c].requirement() != requirementDisabled {
			return fmt.Errorf("condition %q is still active in the OTP sub-flow", execs[c].name())
		}
	}
	return nil
}

func (r *Reconciler) bindBrowserFlow(ctx context.Context, realm, alias string) (Change, error) {
	var cur map[string]any
	if err := r.do(ctx, methodGet, realmPath(realm, ""), nil, &cur); err != nil {
		return ChangeNone, fmt.Errorf("reading realm %q: %w", realm, err)
	}
	if asString(cur["browserFlow"]) == alias {
		return ChangeNone, nil
	}
	if r.dryRun {
		return ChangeUpdated, nil
	}
	if err := r.do(ctx, methodPut, realmPath(realm, ""), map[string]any{"browserFlow": alias}, nil); err != nil {
		return ChangeNone, fmt.Errorf("binding browser flow %q in realm %q: %w", alias, realm, err)
	}
	return ChangeUpdated, nil
}

// --- tree helpers over Keycloak's flattened, depth-first execution list ---

func indexOfProvider(execs []execution, provider string) int {
	for i, e := range execs {
		if e.provider() == provider {
			return i
		}
	}
	return -1
}

// parentOf returns the nearest preceding step one level up, or -1 for
// a top-level step.
func parentOf(execs []execution, i int) int {
	want := execs[i].level() - 1
	if want < 0 {
		return -1
	}
	for j := i - 1; j >= 0; j-- {
		if execs[j].level() == want {
			return j
		}
	}
	return -1
}

// childrenOf lists the direct children of sub-flow p.
func childrenOf(execs []execution, p int) []int {
	lvl := execs[p].level()
	var out []int
	for j := p + 1; j < len(execs) && execs[j].level() > lvl; j++ {
		if execs[j].level() == lvl+1 {
			out = append(out, j)
		}
	}
	return out
}

func isFirstChild(execs []execution, i int) bool {
	p := parentOf(execs, i)
	if p < 0 {
		// Top-level: first among the level-0 steps.
		for j := range i {
			if execs[j].level() == 0 {
				return false
			}
		}
		return true
	}
	return p+1 == i
}

// moveToFirstChild returns a copy of execs with step i (a leaf) moved
// directly under its parent.
func moveToFirstChild(execs []execution, i int) []execution {
	p := parentOf(execs, i)
	out := make([]execution, 0, len(execs))
	for j, e := range execs {
		if j == i {
			continue
		}
		out = append(out, e)
		if j == p {
			out = append(out, execs[i])
		}
	}
	if p < 0 {
		out = append([]execution{execs[i]}, out...)
	}
	return out
}

func copyMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
