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
	testUser     = "wazuh-wui"
	testPass     = "pw"
	adminRole    = "administrator"
	readonlyRole = "readonly"
	errKey       = "error"
)

// fakeWazuh implements the Wazuh API security endpoints the
// reconciler uses, with the stock roles preloaded.
type fakeWazuh struct {
	mu     sync.Mutex
	nextID int
	roles  []*role
	rules  []*rule
	writes int
	logins int
	expire bool
}

func newFakeWazuh() *fakeWazuh {
	f := &fakeWazuh{nextID: 100}
	f.roles = []*role{{ID: 1, Name: adminRole}, {ID: 2, Name: readonlyRole}}
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
	case r.Method == http.MethodGet && p == "/security/roles":
		affected(w, f.roles)
	case r.Method == http.MethodGet && p == "/security/rules":
		affected(w, f.rules)
	case r.Method == http.MethodPost && p == "/security/rules":
		f.nextID++
		name, _ := body[keyName].(string)
		cond, _ := body[keyRule].(map[string]any)
		f.rules = append(f.rules, &rule{ID: f.nextID, Name: name, Rule: cond})
		f.writes++
		affected(w, []map[string]any{{"id": f.nextID}})
	case r.Method == http.MethodPut && strings.HasPrefix(p, "/security/rules/"):
		id, _ := strconv.Atoi(strings.TrimPrefix(p, "/security/rules/"))
		cond, _ := body[keyRule].(map[string]any)
		for _, ru := range f.rules {
			if ru.ID == id {
				ru.Rule = cond
			}
		}
		f.writes++
		affected(w, []map[string]any{{"id": id}})
	case r.Method == http.MethodPost && strings.HasPrefix(p, "/security/roles/") && strings.HasSuffix(p, "/rules"):
		id, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(p, "/security/roles/"), "/rules"))
		for _, part := range strings.Split(r.URL.Query().Get("rule_ids"), ",") {
			n, _ := strconv.Atoi(part)
			for _, ro := range f.roles {
				if ro.ID == id {
					ro.Rules = append(ro.Rules, n)
				}
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
		Tenant: "001",
		Mappings: []RoleMapping{
			{Rule: "oidc_administrator", BackendRole: adminRole, APIRole: adminRole},
			{Rule: "oidc_analyst", BackendRole: "analyst", APIRole: readonlyRole},
			{Rule: RuleName("tenant-001"), BackendRole: "tenant-001", APIRole: readonlyRole},
		},
	}
}

func assertNoErrors(t *testing.T, results []report.Result) {
	t.Helper()
	for _, r := range results {
		assert.NotEqual(t, report.ActionError, r.Action, "%s %s: %s", r.Kind, r.Name, r.Detail)
	}
}

func TestReconcileDryRunApplyIdempotent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFakeWazuh()
	c := newClient(t, f)

	results := Reconcile(ctx, c, spec(), true)
	assert.Zero(t, f.writes, "dry run must not write")
	assertNoErrors(t, results)
	assert.Equal(t, 3*2, report.Summarize(results).Changes, "3 rules created and linked")
	for _, r := range results {
		assert.Equal(t, "wazuh/001", r.Component)
	}

	results = Reconcile(ctx, c, spec(), false)
	assertNoErrors(t, results)
	writes := f.writes
	assert.Equal(t, 3*2, writes)

	results = Reconcile(ctx, c, spec(), false)
	assert.Zero(t, report.Summarize(results).Changes)
	assert.Equal(t, writes, f.writes, "second run must not write")

	assert.Len(t, f.roles[0].Rules, 1, "administrator gets the admin rule")
	assert.Len(t, f.roles[1].Rules, 2, "readonly gets the analyst and tenant rules")
	assert.Equal(t, "oidc_tenant_001", RuleName("tenant-001"))
	assert.Len(t, f.roles, 2, "no roles are created")
}

func TestReconcileRuleDriftAndReauth(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFakeWazuh()
	c := newClient(t, f)
	Reconcile(ctx, c, spec(), false)

	for _, ru := range f.rules {
		if ru.Name == "oidc_tenant_001" {
			ru.Rule = map[string]any{"FIND": map[string]any{keyBackendRoles: "tenant-002"}}
		}
	}
	f.expire = true // the JWT expired between runs
	results := Reconcile(ctx, c, spec(), false)
	var got report.Action
	for _, r := range results {
		if r.Kind == kindRule && r.Name == "oidc_tenant_001" {
			got = r.Action
		}
	}
	assert.Equal(t, report.ActionUpdate, got)
	assert.Equal(t, 2, f.logins, "a 401 triggers one re-login")
	assert.Equal(t, map[string]any{keyBackendRoles: "tenant-001"}, f.rules[2].Rule["FIND"])
}

func TestReconcileMissingAPIRole(t *testing.T) {
	t.Parallel()
	s := spec()
	s.Mappings[2].APIRole = "tenant_viewer"
	results := Reconcile(context.Background(), newClient(t, newFakeWazuh()), s, false)
	last := results[len(results)-1]
	assert.Equal(t, report.ActionError, last.Action)
	assert.Contains(t, last.Detail, "tenant_viewer not found")
}

func TestReconcileBadCredentials(t *testing.T) {
	t.Parallel()
	c := newClient(t, newFakeWazuh())
	c.Password = "wrong"
	results := Reconcile(context.Background(), c, spec(), true)
	require.Len(t, results, 1)
	assert.Equal(t, report.ActionError, results[0].Action)
}
