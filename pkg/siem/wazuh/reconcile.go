// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package wazuh

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/Obmondo/kubeaid-cli/pkg/siem/report"
)

// Component is the report component name.
const Component = "wazuh"

const keyName = "name"

// agentActions are the read actions a tenant gets on its agents.
var agentActions = []string{"agent:read", "sca:read", "syscheck:read", "syscollector:read", "rootcheck:read", "ciscat:read"}

// stockReadPolicies are Wazuh's built-in read-only policies a tenant
// role also gets (cluster/manager status, rules, decoders, CDB lists,
// MITRE). None of them grants access to agents.
var stockReadPolicies = []string{
	"cluster_read_resourceless", "cluster_read_nodes", "rules_read_rules",
	"decoders_read_decoders", "lists_read_rules", "mitre_read_mitre",
}

// RoleMapping maps a backend role (Keycloak realm role in the
// OpenSearch token) to an existing Wazuh API role through a rule.
type RoleMapping struct {
	Rule        string
	BackendRole string
	APIRole     string
}

// Spec is the desired Wazuh API state.
type Spec struct {
	// Operators maps the SOC realm roles to stock API roles.
	Operators []RoleMapping
	// TenantGroups are the tenant group names (prefix + code).
	TenantGroups []string
	// CreateGroups creates missing agent groups.
	CreateGroups bool
}

type policy struct {
	ID     int        `json:"id"`
	Name   string     `json:"name"`
	Policy policyBody `json:"policy"`
}

type policyBody struct {
	Actions   []string `json:"actions"`
	Resources []string `json:"resources"`
	Effect    string   `json:"effect"`
}

type role struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Policies []int  `json:"policies"`
	Rules    []int  `json:"rules"`
}

type rule struct {
	ID   int            `json:"id"`
	Name string         `json:"name"`
	Rule map[string]any `json:"rule"`
}

type group struct {
	Name string `json:"name"`
}

// state is what the API holds, indexed by name.
type state struct {
	policies map[string]policy
	roles    map[string]role
	rules    map[string]rule
	groups   map[string]bool
}

// run collects results for one reconcile pass.
type run struct {
	c       *Client
	dryRun  bool
	st      state
	results []report.Result
}

func (r *run) add(kind, name string, a report.Action, detail string) {
	r.results = append(r.results, report.Result{Component: Component, Kind: kind, Name: name, Action: a, Detail: detail})
}

// RuleName returns the rule name for a tenant group:
// "oidc_" + group with dashes as underscores.
func RuleName(group string) string { return "oidc_" + strings.ReplaceAll(group, "-", "_") }

// Reconcile brings the Wazuh API RBAC in line with spec.
func Reconcile(ctx context.Context, c *Client, spec Spec, dryRun bool) []report.Result {
	r := &run{c: c, dryRun: dryRun}
	if err := r.load(ctx); err != nil {
		r.add("api", "state", report.ActionError, err.Error())
		return r.results
	}

	for _, m := range spec.Operators {
		apiRole, ok := r.st.roles[m.APIRole]
		if !ok {
			r.add("rule", m.Rule, report.ActionError, "API role "+m.APIRole+" not found")
			continue
		}
		ruleID, ok := r.ensureRule(ctx, m.Rule, m.BackendRole)
		if ok {
			r.ensureRoleRules(ctx, apiRole, ruleID)
		}
	}
	if len(spec.TenantGroups) == 0 {
		return r.results
	}

	var cfg struct {
		RBACMode string `json:"rbac_mode"`
	}
	if err := c.call(ctx, http.MethodGet, "/security/config", nil, &cfg); err != nil {
		r.add("api", "security-config", report.ActionError, err.Error())
		return r.results
	}
	if cfg.RBACMode != "white" {
		r.add("api", "rbac_mode", report.ActionError,
			fmt.Sprintf("rbac_mode is %q, not white: tenant roles would not restrict anything; tenant RBAC skipped", cfg.RBACMode))
		return r.results
	}
	stock := make([]int, 0, len(stockReadPolicies))
	for _, n := range stockReadPolicies {
		p, ok := r.st.policies[n]
		if !ok {
			r.add("policy", n, report.ActionError, "stock policy not found; tenant RBAC skipped")
			return r.results
		}
		stock = append(stock, p.ID)
	}
	for _, g := range spec.TenantGroups {
		r.reconcileTenant(ctx, g, stock, spec.CreateGroups)
	}
	return r.results
}

func (r *run) load(ctx context.Context) error {
	policies, err := items[policy](ctx, r.c, "/security/policies?limit=500")
	if err != nil {
		return err
	}
	roles, err := items[role](ctx, r.c, "/security/roles?limit=500")
	if err != nil {
		return err
	}
	rules, err := items[rule](ctx, r.c, "/security/rules?limit=500")
	if err != nil {
		return err
	}
	groups, err := items[group](ctx, r.c, "/groups?limit=500")
	if err != nil {
		return err
	}
	r.st = state{policies: map[string]policy{}, roles: map[string]role{}, rules: map[string]rule{}, groups: map[string]bool{}}
	for _, p := range policies {
		r.st.policies[p.Name] = p
	}
	for _, ro := range roles {
		r.st.roles[ro.Name] = ro
	}
	for _, ru := range rules {
		r.st.rules[ru.Name] = ru
	}
	for _, g := range groups {
		r.st.groups[g.Name] = true
	}
	return nil
}

func (r *run) reconcileTenant(ctx context.Context, g string, stock []int, createGroups bool) {
	switch {
	case r.st.groups[g]:
		r.add("agent-group", g, report.ActionOK, "")
	case !createGroups:
		r.add("agent-group", g, report.ActionSkip, "missing; set createGroups or sync the agent group config")
	case r.dryRun:
		r.add("agent-group", g, report.ActionCreate, "")
	default:
		if err := r.c.call(ctx, http.MethodPost, "/groups", map[string]string{"group_id": g}, nil); err != nil {
			r.add("agent-group", g, report.ActionError, err.Error())
		} else {
			r.add("agent-group", g, report.ActionCreate, "")
		}
	}

	agents, ok1 := r.ensurePolicy(ctx, g+"_agents", policyBody{Actions: agentActions, Resources: []string{"agent:group:" + g}, Effect: "allow"})
	grp, ok2 := r.ensurePolicy(ctx, g+"_group", policyBody{Actions: []string{"group:read"}, Resources: []string{"group:id:" + g}, Effect: "allow"})
	ro, ok3 := r.ensureRole(ctx, g+"_readonly")
	if !ok1 || !ok2 || !ok3 {
		return
	}
	r.ensureRolePolicies(ctx, ro, append([]int{agents, grp}, stock...))
	if ruleID, ok := r.ensureRule(ctx, RuleName(g), g); ok {
		r.ensureRoleRules(ctx, ro, ruleID)
	}
}

// ensurePolicy returns the policy id (0 in dry-run when it would be
// created) and whether reconciliation can continue.
func (r *run) ensurePolicy(ctx context.Context, name string, want policyBody) (int, bool) {
	cur, ok := r.st.policies[name]
	if !ok {
		if r.dryRun {
			r.add("policy", name, report.ActionCreate, "")
			return 0, true
		}
		id, err := r.c.createdID(ctx, "/security/policies", map[string]any{keyName: name, "policy": want})
		if err != nil {
			r.add("policy", name, report.ActionError, err.Error())
			return 0, false
		}
		r.add("policy", name, report.ActionCreate, "")
		return id, true
	}
	if samePolicy(cur.Policy, want) {
		r.add("policy", name, report.ActionOK, "")
		return cur.ID, true
	}
	if !r.dryRun {
		if err := r.c.call(ctx, http.MethodPut, "/security/policies/"+strconv.Itoa(cur.ID), map[string]any{"policy": want}, nil); err != nil {
			r.add("policy", name, report.ActionError, err.Error())
			return cur.ID, false
		}
	}
	r.add("policy", name, report.ActionUpdate, "actions/resources/effect")
	return cur.ID, true
}

func (r *run) ensureRole(ctx context.Context, name string) (role, bool) {
	if cur, ok := r.st.roles[name]; ok {
		r.add("role", name, report.ActionOK, "")
		return cur, true
	}
	if r.dryRun {
		r.add("role", name, report.ActionCreate, "")
		return role{Name: name}, true
	}
	id, err := r.c.createdID(ctx, "/security/roles", map[string]string{keyName: name})
	if err != nil {
		r.add("role", name, report.ActionError, err.Error())
		return role{}, false
	}
	r.add("role", name, report.ActionCreate, "")
	return role{ID: id, Name: name}, true
}

// ensureRule returns the rule id (0 in dry-run when it would be
// created).
func (r *run) ensureRule(ctx context.Context, name, backendRole string) (int, bool) {
	want := map[string]any{"FIND": map[string]any{"backend_roles": backendRole}}
	cur, ok := r.st.rules[name]
	if !ok {
		if r.dryRun {
			r.add("rule", name, report.ActionCreate, "")
			return 0, true
		}
		id, err := r.c.createdID(ctx, "/security/rules", map[string]any{keyName: name, "rule": want})
		if err != nil {
			r.add("rule", name, report.ActionError, err.Error())
			return 0, false
		}
		r.add("rule", name, report.ActionCreate, "")
		return id, true
	}
	if sameJSON(cur.Rule, want) {
		r.add("rule", name, report.ActionOK, "")
		return cur.ID, true
	}
	if !r.dryRun {
		if err := r.c.call(ctx, http.MethodPut, "/security/rules/"+strconv.Itoa(cur.ID), map[string]any{"rule": want}, nil); err != nil {
			r.add("rule", name, report.ActionError, err.Error())
			return cur.ID, false
		}
	}
	r.add("rule", name, report.ActionUpdate, "rule condition")
	return cur.ID, true
}

// ensureRolePolicies links missing policies to the role. Links are
// never removed.
func (r *run) ensureRolePolicies(ctx context.Context, ro role, want []int) {
	r.link(ctx, "role-policies", ro, ro.Policies, want, "policies", "policy_ids")
}

func (r *run) ensureRoleRules(ctx context.Context, ro role, ruleID int) {
	r.link(ctx, "role-rules", ro, ro.Rules, []int{ruleID}, "rules", "rule_ids")
}

func (r *run) link(ctx context.Context, kind string, ro role, have, want []int, what, param string) {
	var add []string
	pending := ro.ID == 0 // role not created yet (dry run)
	for _, id := range want {
		if id == 0 {
			pending = true
			continue
		}
		if !containsInt(have, id) {
			add = append(add, strconv.Itoa(id))
		}
	}
	switch {
	case len(add) == 0 && !pending:
		r.add(kind, ro.Name, report.ActionOK, "")
		return
	case r.dryRun:
		r.add(kind, ro.Name, report.ActionUpdate, "link "+what)
		return
	}
	path := fmt.Sprintf("/security/roles/%d/%s?%s=%s", ro.ID, what, param, url.QueryEscape(strings.Join(add, ",")))
	if err := r.c.call(ctx, http.MethodPost, path, nil, nil); err != nil {
		r.add(kind, ro.Name, report.ActionError, err.Error())
		return
	}
	r.add(kind, ro.Name, report.ActionUpdate, "link "+what+" "+strings.Join(add, ","))
}

func samePolicy(a, b policyBody) bool {
	return a.Effect == b.Effect && sameSet(a.Actions, b.Actions) && sameSet(a.Resources, b.Resources)
}

func sameSet(a, b []string) bool {
	x := append([]string(nil), a...)
	y := append([]string(nil), b...)
	sort.Strings(x)
	sort.Strings(y)
	return reflect.DeepEqual(x, y)
}

func sameJSON(a, b any) bool {
	x, err1 := json.Marshal(a)
	y, err2 := json.Marshal(b)
	return err1 == nil && err2 == nil && string(x) == string(y)
}

func containsInt(list []int, v int) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
