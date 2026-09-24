// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package wazuh

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-cli/pkg/siem/report"
)

const (
	testUser  = "wazuh-wui"
	testPass  = "pw"
	adminRole = "administrator"
	errKey    = "error"
)

// fakeWazuh implements the Wazuh API security endpoints the
// reconciler uses, with stock roles and policies preloaded.
type fakeWazuh struct {
	mu       sync.Mutex
	nextID   int
	policies []*policy
	roles    []*role
	rules    []*rule
	groups   []string
	rbacMode string
	writes   int
	logins   int
	expire   bool
}

func newFakeWazuh() *fakeWazuh {
	f := &fakeWazuh{nextID: 100, rbacMode: "white", groups: []string{"default"}}
	f.roles = []*role{{ID: 1, Name: adminRole}, {ID: 2, Name: "readonly"}}
	for i, n := range stockReadPolicies {
		f.policies = append(f.policies, &policy{ID: 10 + i, Name: n})
	}
	return f
}

func envelope(w http.ResponseWriter, data any) {
	_ = json.NewEncoder(w).Encode(map[string]any{"data": data, errKey: 0, "message": "ok"})
}

func affected(w http.ResponseWriter, list any) {
	envelope(w, map[string]any{"affected_items": list, "failed_items": []any{}})
}

//nolint:gocognit,gocyclo // a single fake router
func (f *fakeWazuh) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.URL.Path == "/security/user/authenticate" {
		u, p, _ := r.BasicAuth()
		if u != testUser || p != testPass {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		f.logins++
		_, _ = w.Write([]byte("jwt-" + strconv.Itoa(f.logins)))
		return
	}
	if f.expire || r.Header.Get("Authorization") != "Bearer jwt-"+strconv.Itoa(f.logins) {
		f.expire = false
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{"title": "Unauthorized", errKey: 6})
		return
	}
	var body map[string]any
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	p := r.URL.Path
	switch {
	case r.Method == http.MethodGet && p == "/security/config":
		envelope(w, map[string]any{"rbac_mode": f.rbacMode})
	case r.Method == http.MethodGet && p == "/security/policies":
		affected(w, f.policies)
	case r.Method == http.MethodGet && p == "/security/roles":
		affected(w, f.roles)
	case r.Method == http.MethodGet && p == "/security/rules":
		affected(w, f.rules)
	case r.Method == http.MethodGet && p == "/groups":
		list := []map[string]any{}
		for _, g := range f.groups {
			list = append(list, map[string]any{keyName: g})
		}
		affected(w, list)
	case r.Method == http.MethodPost && p == "/groups":
		gid, _ := body["group_id"].(string)
		f.groups = append(f.groups, gid)
		f.writes++
		envelope(w, map[string]any{})
	case r.Method == http.MethodPost && p == "/security/policies":
		f.nextID++
		raw, _ := json.Marshal(body["policy"])
		var pb policyBody
		_ = json.Unmarshal(raw, &pb)
		name, _ := body[keyName].(string)
		f.policies = append(f.policies, &policy{ID: f.nextID, Name: name, Policy: pb})
		f.writes++
		affected(w, []map[string]any{{"id": f.nextID}})
	case r.Method == http.MethodPut && strings.HasPrefix(p, "/security/policies/"):
		id, _ := strconv.Atoi(strings.TrimPrefix(p, "/security/policies/"))
		raw, _ := json.Marshal(body["policy"])
		for _, pol := range f.policies {
			if pol.ID == id {
				_ = json.Unmarshal(raw, &pol.Policy)
			}
		}
		f.writes++
		affected(w, []map[string]any{{"id": id}})
	case r.Method == http.MethodPost && p == "/security/roles":
		f.nextID++
		name, _ := body[keyName].(string)
		f.roles = append(f.roles, &role{ID: f.nextID, Name: name})
		f.writes++
		affected(w, []map[string]any{{"id": f.nextID}})
	case r.Method == http.MethodPost && p == "/security/rules":
		f.nextID++
		name, _ := body[keyName].(string)
		cond, _ := body["rule"].(map[string]any)
		f.rules = append(f.rules, &rule{ID: f.nextID, Name: name, Rule: cond})
		f.writes++
		affected(w, []map[string]any{{"id": f.nextID}})
	case r.Method == http.MethodPost && strings.HasPrefix(p, "/security/roles/"):
		parts := strings.Split(strings.TrimPrefix(p, "/security/roles/"), "/")
		id, _ := strconv.Atoi(parts[0])
		var ids []int
		for _, s := range strings.Split(r.URL.Query().Get(map[string]string{"policies": "policy_ids", "rules": "rule_ids"}[parts[1]]), ",") {
			n, _ := strconv.Atoi(s)
			ids = append(ids, n)
		}
		for _, ro := range f.roles {
			if ro.ID == id && parts[1] == "policies" {
				ro.Policies = append(ro.Policies, ids...)
			}
			if ro.ID == id && parts[1] == "rules" {
				ro.Rules = append(ro.Rules, ids...)
			}
		}
		f.writes++
		affected(w, []any{})
	default:
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{errKey: 1, "message": "no route " + p})
	}
}

func newClient(t *testing.T, f *fakeWazuh) *Client {
	t.Helper()
	srv := httptest.NewTLSServer(f)
	t.Cleanup(srv.Close)
	return &Client{BaseURL: srv.URL, Username: testUser, Password: testPass, HTTP: srv.Client()}
}

func spec() Spec {
	return Spec{
		Operators: []RoleMapping{
			{Rule: "oidc_administrator", BackendRole: adminRole, APIRole: adminRole},
			{Rule: "oidc_analyst", BackendRole: "analyst", APIRole: "readonly"},
		},
		TenantGroups: []string{"tenant-001", "tenant-002"},
		CreateGroups: true,
	}
}

func TestReconcileDryRunApplyIdempotent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFakeWazuh()
	c := newClient(t, f)

	results := Reconcile(ctx, c, spec(), true)
	assert.Zero(t, f.writes, "dry run must not write")
	for _, r := range results {
		assert.NotEqual(t, report.ActionError, r.Action, "%s %s: %s", r.Kind, r.Name, r.Detail)
	}
	assert.Equal(t, 2*7+4, report.Summarize(results).Changes)

	results = Reconcile(ctx, c, spec(), false)
	for _, r := range results {
		assert.NotEqual(t, report.ActionError, r.Action, "%s %s: %s", r.Kind, r.Name, r.Detail)
	}
	assert.Contains(t, f.groups, "tenant-002")
	writes := f.writes

	results = Reconcile(ctx, c, spec(), false)
	assert.Zero(t, report.Summarize(results).Changes)
	assert.Equal(t, writes, f.writes, "second run must not write")

	var ro *role
	for _, x := range f.roles {
		if x.Name == "tenant-001_readonly" {
			ro = x
		}
	}
	require.NotNil(t, ro)
	assert.Len(t, ro.Policies, 2+len(stockReadPolicies))
	assert.Len(t, ro.Rules, 1)
	assert.Equal(t, "oidc_tenant_001", RuleName("tenant-001"))
}

func TestReconcilePolicyDriftAndReauth(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFakeWazuh()
	c := newClient(t, f)
	Reconcile(ctx, c, spec(), false)

	for _, p := range f.policies {
		if p.Name == "tenant-001_agents" {
			p.Policy.Actions = []string{"agent:read", "agent:delete"}
		}
	}
	f.expire = true // the JWT expired between runs
	results := Reconcile(ctx, c, spec(), false)
	var got report.Action
	for _, r := range results {
		if r.Name == "tenant-001_agents" {
			got = r.Action
		}
	}
	assert.Equal(t, report.ActionUpdate, got)
	assert.Equal(t, 2, f.logins, "a 401 triggers one re-login")
}

func TestReconcileRefusesBlackRBAC(t *testing.T) {
	t.Parallel()
	f := newFakeWazuh()
	f.rbacMode = "black"
	results := Reconcile(context.Background(), newClient(t, f), spec(), false)
	last := results[len(results)-1]
	assert.Equal(t, report.ActionError, last.Action)
	assert.Contains(t, last.Detail, "rbac_mode")
}

func TestReconcileBadCredentials(t *testing.T) {
	t.Parallel()
	c := newClient(t, newFakeWazuh())
	c.Password = "wrong"
	results := Reconcile(context.Background(), c, spec(), true)
	require.Len(t, results, 1)
	assert.Equal(t, report.ActionError, results[0].Action)
}

func TestReconcileMissingGroupWithoutCreate(t *testing.T) {
	t.Parallel()
	f := newFakeWazuh()
	s := spec()
	s.CreateGroups = false
	results := Reconcile(context.Background(), newClient(t, f), s, false)
	for _, r := range results {
		if r.Kind == "agent-group" {
			assert.Equal(t, report.ActionSkip, r.Action)
		}
	}
}
