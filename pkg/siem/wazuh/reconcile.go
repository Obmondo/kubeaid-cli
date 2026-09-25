// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package wazuh

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/Obmondo/kubeaid-cli/pkg/siem/report"
)

// Component is the report component name; results carry
// ComponentName(tenant).
const Component = "wazuh"

const (
	keyName         = "name"
	keyRule         = "rule"
	keyBackendRoles = "backend_roles"
	kindRule        = "rule"
)

// ComponentName returns the report component name for one tenant's
// manager, e.g. "wazuh/001".
func ComponentName(tenant string) string {
	if tenant == "" {
		return Component
	}
	return Component + "/" + tenant
}

// RoleMapping maps a backend role (Keycloak realm role in the
// OpenSearch token) to an existing Wazuh API role through a rule.
type RoleMapping struct {
	Rule        string
	BackendRole string
	APIRole     string
}

// Spec is the desired RBAC of one tenant's manager.
type Spec struct {
	// Tenant is the tenant code, used in the report component name.
	Tenant string
	// Mappings are the operator roles and the tenant group, each
	// mapped to a stock or pre-existing API role.
	Mappings []RoleMapping
}

type role struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Rules []int  `json:"rules"`
}

type rule struct {
	ID   int            `json:"id"`
	Name string         `json:"name"`
	Rule map[string]any `json:"rule"`
}

// state is what the API holds, indexed by name.
type state struct {
	roles map[string]role
	rules map[string]rule
}

// run collects results for one reconcile pass.
type run struct {
	c         *Client
	dryRun    bool
	component string
	st        state
	results   []report.Result
}

func (r *run) add(kind, name string, a report.Action, detail string) {
	r.results = append(r.results, report.Result{Component: r.component, Kind: kind, Name: name, Action: a, Detail: detail})
}

// RuleName returns the rule name for a backend role:
// "oidc_" + role with dashes as underscores.
func RuleName(backendRole string) string { return "oidc_" + strings.ReplaceAll(backendRole, "-", "_") }

// Reconcile brings one manager's API RBAC in line with spec.
func Reconcile(ctx context.Context, c *Client, spec Spec, dryRun bool) []report.Result {
	r := &run{c: c, dryRun: dryRun, component: ComponentName(spec.Tenant)}
	if err := r.load(ctx); err != nil {
		r.add("api", "state", report.ActionError, err.Error())
		return r.results
	}
	for _, m := range spec.Mappings {
		apiRole, ok := r.st.roles[m.APIRole]
		if !ok {
			r.add(kindRule, m.Rule, report.ActionError, "API role "+m.APIRole+" not found")
			continue
		}
		if ruleID, ok := r.ensureRule(ctx, m.Rule, m.BackendRole); ok {
			r.ensureRoleRule(ctx, apiRole, m.Rule, ruleID)
		}
	}
	return r.results
}

func (r *run) load(ctx context.Context) error {
	roles, err := items[role](ctx, r.c, "/security/roles?limit=500")
	if err != nil {
		return err
	}
	rules, err := items[rule](ctx, r.c, "/security/rules?limit=500")
	if err != nil {
		return err
	}
	r.st = state{roles: map[string]role{}, rules: map[string]rule{}}
	for _, ro := range roles {
		r.st.roles[ro.Name] = ro
	}
	for _, ru := range rules {
		r.st.rules[ru.Name] = ru
	}
	return nil
}

// ensureRule returns the rule id (0 in dry-run when it would be
// created).
func (r *run) ensureRule(ctx context.Context, name, backendRole string) (int, bool) {
	want := map[string]any{"FIND": map[string]any{keyBackendRoles: backendRole}}
	cur, ok := r.st.rules[name]
	if !ok {
		if r.dryRun {
			r.add(kindRule, name, report.ActionCreate, "")
			return 0, true
		}
		id, err := r.c.createdID(ctx, "/security/rules", map[string]any{keyName: name, keyRule: want})
		if err != nil {
			r.add(kindRule, name, report.ActionError, err.Error())
			return 0, false
		}
		r.add(kindRule, name, report.ActionCreate, "")
		return id, true
	}
	if sameJSON(cur.Rule, want) {
		r.add(kindRule, name, report.ActionOK, "")
		return cur.ID, true
	}
	if !r.dryRun {
		if err := r.c.call(ctx, http.MethodPut, "/security/rules/"+strconv.Itoa(cur.ID), map[string]any{keyRule: want}, nil); err != nil {
			r.add(kindRule, name, report.ActionError, err.Error())
			return cur.ID, false
		}
	}
	r.add(kindRule, name, report.ActionUpdate, "rule condition")
	return cur.ID, true
}

// ensureRoleRule links the rule to the API role, reported as
// "<role>/<rule>". Links are never removed.
func (r *run) ensureRoleRule(ctx context.Context, ro role, ruleName string, ruleID int) {
	name := ro.Name + "/" + ruleName
	switch {
	case ruleID != 0 && slices.Contains(ro.Rules, ruleID):
		r.add("role-rule", name, report.ActionOK, "")
		return
	case r.dryRun:
		r.add("role-rule", name, report.ActionUpdate, "link rule")
		return
	}
	id := strconv.Itoa(ruleID)
	path := fmt.Sprintf("/security/roles/%d/rules?rule_ids=%s", ro.ID, url.QueryEscape(id))
	if err := r.c.call(ctx, http.MethodPost, path, nil, nil); err != nil {
		r.add("role-rule", name, report.ActionError, err.Error())
		return
	}
	r.add("role-rule", name, report.ActionUpdate, "link rule id "+id)
}

func sameJSON(a, b any) bool {
	x, err1 := json.Marshal(a)
	y, err2 := json.Marshal(b)
	return err1 == nil && err2 == nil && string(x) == string(y)
}
