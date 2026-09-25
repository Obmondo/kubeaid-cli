// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package iris

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
	analysts    = "Analysts"
	automation  = "Automation"
	customerID  = "customer_id"
	testKey     = "test-key"
	initialCust = "IrisInitialClient"
	svcLogin    = "svc_ai"
	userID      = "user_id"
)

// fakeIRIS implements the handful of IRIS admin endpoints the
// reconciler uses, with IRIS's {"status","data"} envelope.
type fakeIRIS struct {
	mu        sync.Mutex
	customers map[string]int
	groups    map[string]int
	users     map[string]*fakeUser
	nextID    int
	writes    int
	// userKeys are the API keys of non-admin users (login -> key).
	userKeys map[string]string
	renewals int
}

type fakeUser struct {
	id        int
	groups    []int
	customers []int
}

func newFakeIRIS() *fakeIRIS {
	return &fakeIRIS{
		customers: map[string]int{initialCust: 1},
		groups:    map[string]int{"Administrators": 1, analysts: 2, automation: 3},
		users:     map[string]*fakeUser{svcLogin: {id: 7, groups: []int{3}, customers: []int{1}}},
		nextID:    10,
		userKeys:  map[string]string{svcLogin: "svc-key-1"},
	}
}

func ok(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "message": "", "data": data})
}

func fail(w http.ResponseWriter, code int, msg string) {
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "error", "message": msg, "data": []any{}})
}

func (f *fakeIRIS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := r.URL.Path
	if p == "/api/ping" {
		for _, k := range f.userKeys {
			if r.Header.Get("Authorization") == "Bearer "+k {
				ok(w, nil)
				return
			}
		}
	}
	if r.Header.Get("Authorization") != "Bearer "+testKey {
		fail(w, http.StatusUnauthorized, "bad key")
		return
	}
	switch {
	case p == "/api/ping":
		ok(w, nil)
	case p == "/manage/users/add" && r.Method == http.MethodPost:
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		login, _ := body["user_login"].(string)
		if pw, _ := body["user_password"].(string); len(pw) < 12 {
			fail(w, http.StatusInternalServerError, "missing 1 required positional argument: 'password'")
			return
		}
		if sa, _ := body["user_is_service_account"].(bool); !sa {
			fail(w, http.StatusBadRequest, "expected a service account")
			return
		}
		f.nextID++
		f.users[login] = &fakeUser{id: f.nextID}
		f.userKeys[login] = login + "-created-key"
		f.writes++
		ok(w, map[string]any{userID: f.nextID, "user_api_key": f.userKeys[login]})
	case strings.HasPrefix(p, "/manage/users/renew-api-key/") && r.Method == http.MethodPost:
		id, _ := strconv.Atoi(strings.TrimPrefix(p, "/manage/users/renew-api-key/"))
		for login, u := range f.users {
			if u.id == id {
				f.renewals++
				f.userKeys[login] = login + "-renewed-" + strconv.Itoa(f.renewals)
				f.writes++
				ok(w, map[string]any{userID: id, "user_api_key": f.userKeys[login]})
				return
			}
		}
		fail(w, http.StatusBadRequest, "no user")
	case p == "/manage/customers/list":
		list := []map[string]any{}
		for n, id := range f.customers {
			list = append(list, map[string]any{"customer_name": n, customerID: id})
		}
		ok(w, list)
	case p == "/manage/customers/add" && r.Method == http.MethodPost:
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		name, _ := body["customer_name"].(string)
		f.nextID++
		f.customers[name] = f.nextID
		f.writes++
		ok(w, map[string]any{customerID: f.nextID})
	case p == "/manage/groups/list":
		list := []map[string]any{}
		for n, id := range f.groups {
			list = append(list, map[string]any{"group_name": n, "group_id": id})
		}
		ok(w, list)
	case strings.HasPrefix(p, "/manage/users/lookup/login/"):
		u, found := f.users[strings.TrimPrefix(p, "/manage/users/lookup/login/")]
		if !found {
			fail(w, http.StatusBadRequest, "User not found")
			return
		}
		ok(w, map[string]any{userID: u.id})
	case strings.HasPrefix(p, "/manage/users/"):
		f.handleUser(w, r, strings.Split(strings.TrimPrefix(p, "/manage/users/"), "/"))
	default:
		fail(w, http.StatusNotFound, "no route "+p)
	}
}

func (f *fakeIRIS) handleUser(w http.ResponseWriter, r *http.Request, parts []string) {
	id, _ := strconv.Atoi(parts[0])
	var u *fakeUser
	for _, cand := range f.users {
		if cand.id == id {
			u = cand
		}
	}
	if u == nil {
		fail(w, http.StatusBadRequest, "no user")
		return
	}
	if len(parts) == 1 {
		groups, customers := []map[string]any{}, []map[string]any{}
		for _, g := range u.groups {
			groups = append(groups, map[string]any{"group_id": g})
		}
		for _, c := range u.customers {
			customers = append(customers, map[string]any{customerID: c})
		}
		ok(w, map[string]any{userID: u.id, "user_groups": groups, "user_customers": customers})
		return
	}
	var body map[string][]int
	_ = json.NewDecoder(r.Body).Decode(&body)
	switch parts[1] {
	case "groups":
		u.groups = body["groups_membership"]
	case "customers":
		u.customers = body["customers_membership"]
	}
	f.writes++
	ok(w, nil)
}

func newClient(t *testing.T, f *fakeIRIS) *Client {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return &Client{BaseURL: srv.URL, APIKey: testKey, HTTP: srv.Client()}
}

func spec() Spec {
	return Spec{
		Customers:       []Customer{{Name: "Tenant A", Description: "tenant 001"}, {Name: "Tenant B"}},
		InitialCustomer: initialCust,
		ServiceAccounts: []ServiceAccount{{Login: svcLogin, Groups: []string{automation}}, {Login: "svc_missing"}},
	}
}

func actions(results []report.Result) map[string]report.Action {
	out := map[string]report.Action{}
	for _, r := range results {
		out[r.Kind+"/"+r.Name] = r.Action
	}
	return out
}

func TestReconcileDryRunApplyIdempotent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFakeIRIS()
	c := newClient(t, f)

	got := actions(Reconcile(ctx, c, spec(), true))
	assert.Equal(t, report.ActionCreate, got["customer/Tenant A"])
	assert.Equal(t, report.ActionOK, got["service-account-groups/"+svcLogin])
	assert.Equal(t, report.ActionUpdate, got["service-account-customers/"+svcLogin])
	assert.Equal(t, report.ActionSkip, got["service-account/svc_missing"])
	assert.Equal(t, 0, f.writes, "dry run must not write")

	got = actions(Reconcile(ctx, c, spec(), false))
	assert.Equal(t, report.ActionCreate, got["customer/Tenant B"])
	assert.Equal(t, report.ActionUpdate, got["service-account-customers/"+svcLogin])
	assert.Len(t, f.users[svcLogin].customers, 3)
	assert.Equal(t, []int{3}, f.users[svcLogin].groups)
	writes := f.writes

	for _, r := range Reconcile(ctx, c, spec(), false) {
		assert.False(t, r.IsChange(), "%s %s", r.Kind, r.Name)
	}
	assert.Equal(t, writes, f.writes, "second run must not write")
}

func TestReconcileKeepsExtraMembershipsAndReportsUnknownGroup(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFakeIRIS()
	f.users[svcLogin].customers = []int{1, 99} // 99: a customer added by hand
	c := newClient(t, f)

	s := spec()
	s.ServiceAccounts[0].Groups = []string{automation, analysts, "Nope"}
	got := actions(Reconcile(ctx, c, s, false))
	assert.Equal(t, report.ActionError, got["service-account-groups/"+svcLogin])
	assert.Contains(t, f.users[svcLogin].customers, 99, "hand-added customers are kept")

	s.ServiceAccounts[0].Groups = []string{automation, analysts}
	got = actions(Reconcile(ctx, c, s, false))
	assert.Equal(t, report.ActionUpdate, got["service-account-groups/"+svcLogin])
	assert.ElementsMatch(t, []int{2, 3}, f.users[svcLogin].groups)
}

func TestReconcileAPIErrors(t *testing.T) {
	t.Parallel()
	c := newClient(t, newFakeIRIS())
	c.APIKey = "wrong"
	results := Reconcile(context.Background(), c, spec(), true)
	require.Len(t, results, 1)
	assert.Equal(t, report.ActionError, results[0].Action)
	assert.Contains(t, results[0].Detail, "HTTP 401")
}
