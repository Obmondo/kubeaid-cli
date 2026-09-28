// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package misp

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
	adminKey    = "admin-key"
	exportEmail = "ioc-export@example.com"
	readOnly    = "Read Only"
	roleUser    = "User"
	orgOther    = "Other"
	userModel   = "User"
)

// num reads a JSON number from a decoded body (0 when absent).
func num(body map[string]any, key string) int {
	f, _ := body[key].(float64)
	return int(f)
}

type fakeUser struct {
	id, org, role int
	email, key    string
	disabled      bool
}

// fakeMISP answers the handful of MISP endpoints the reconciler uses, in
// MISP's shapes ({"User": {...}} rows, ids as strings).
type fakeMISP struct {
	mu     sync.Mutex
	users  []*fakeUser
	roles  map[string]int
	orgs   map[string]int
	writes int
	resets int
}

func newFakeMISP() *fakeMISP {
	return &fakeMISP{
		users: []*fakeUser{{id: 1, org: 1, role: 1, email: "admin@admin.test", key: adminKey}},
		roles: map[string]int{"admin": 1, roleUser: 3, readOnly: 6},
		orgs:  map[string]int{"ORGNAME": 1, orgOther: 2},
	}
}

func userJSON(u *fakeUser) map[string]any {
	return map[string]any{
		"id": strconv.Itoa(u.id), "email": u.email, "org_id": strconv.Itoa(u.org),
		fieldRoleID: strconv.Itoa(u.role), "disabled": u.disabled,
	}
}

func (f *fakeMISP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var caller *fakeUser
	for _, u := range f.users {
		if r.Header.Get("Authorization") == u.key && !u.disabled {
			caller = u
		}
	}
	if caller == nil {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"name":"Authentication failed."}`))
		return
	}
	reply := func(v any) { _ = json.NewEncoder(w).Encode(v) }
	p := r.URL.Path
	if p == "/users/view/me" {
		reply(map[string]any{userModel: userJSON(caller)})
		return
	}
	if caller.role != 1 {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	switch {
	case p == "/roles/index":
		out := []map[string]any{}
		for n, id := range f.roles {
			out = append(out, map[string]any{"Role": map[string]any{"id": strconv.Itoa(id), fieldName: n}})
		}
		reply(out)
	case p == "/organisations/index":
		out := []map[string]any{}
		for n, id := range f.orgs {
			out = append(out, map[string]any{"Organisation": map[string]any{"id": strconv.Itoa(id), fieldName: n}})
		}
		reply(out)
	case p == "/admin/users/index":
		out := []map[string]any{}
		for _, u := range f.users {
			out = append(out, map[string]any{userModel: userJSON(u)})
		}
		reply(out)
	case p == "/admin/users/add" && r.Method == http.MethodPost:
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if pw, _ := body["password"].(string); len(pw) < 12 {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		email, _ := body["email"].(string)
		u := &fakeUser{id: len(f.users) + 10, email: email, org: num(body, "org_id"), role: num(body, fieldRoleID)}
		u.key = "created-" + strconv.Itoa(u.id)
		f.users = append(f.users, u)
		f.writes++
		j := userJSON(u)
		j["authkey"] = u.key
		reply(map[string]any{userModel: j})
	case strings.HasPrefix(p, "/admin/users/edit/") && r.Method == http.MethodPost:
		id, _ := strconv.Atoi(strings.TrimPrefix(p, "/admin/users/edit/"))
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		for _, u := range f.users {
			if u.id == id {
				u.role = num(body, fieldRoleID)
			}
		}
		f.writes++
		reply(map[string]any{userModel: map[string]any{"id": strconv.Itoa(id)}})
	case strings.HasPrefix(p, "/users/resetauthkey/") && r.Method == http.MethodPost:
		id, _ := strconv.Atoi(strings.TrimPrefix(p, "/users/resetauthkey/"))
		for _, u := range f.users {
			if u.id == id {
				f.resets++
				u.key = "reset-" + strconv.Itoa(f.resets)
				f.writes++
				msg := "Authkey updated: " + u.key
				reply(map[string]any{"saved": true, "success": true, "name": msg, "message": msg})
				return
			}
		}
		w.WriteHeader(http.StatusNotFound)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (f *fakeMISP) byEmail(email string) *fakeUser {
	for _, u := range f.users {
		if u.email == email {
			return u
		}
	}
	return nil
}

type memKeyStore struct{ key string }

func (m *memKeyStore) Get(context.Context) (string, error) { return m.key, nil }

func (m *memKeyStore) Put(_ context.Context, key string, dryRun bool) (report.Action, error) {
	if key == m.key {
		return report.ActionOK, nil
	}
	if !dryRun {
		m.key = key
	}
	return report.ActionUpdate, nil
}

func newClient(t *testing.T, f *fakeMISP) *Client {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return &Client{BaseURL: srv.URL, APIKey: adminKey, HTTP: srv.Client()}
}

func actions(results []report.Result) map[string]report.Action {
	out := map[string]report.Action{}
	for _, r := range results {
		out[r.Kind+"/"+r.Name] = r.Action
	}
	return out
}

func TestReadOnlyUserCreatedWithKey(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFakeMISP()
	c := newClient(t, f)
	store := &memKeyStore{}
	spec := Spec{Users: []User{{Email: exportEmail, Role: readOnly, Key: store}}}

	dry := actions(Reconcile(ctx, c, spec, true))
	assert.Equal(t, report.ActionCreate, dry["user/"+exportEmail])
	assert.Equal(t, report.ActionCreate, dry["user-key/"+exportEmail])
	assert.Equal(t, 0, f.writes, "dry run must not write")

	got := actions(Reconcile(ctx, c, spec, false))
	assert.Equal(t, report.ActionCreate, got["user/"+exportEmail])
	assert.Equal(t, report.ActionUpdate, got["user-key/"+exportEmail])
	u := f.byEmail(exportEmail)
	require.NotNil(t, u)
	assert.Equal(t, 6, u.role)
	assert.Equal(t, 1, u.org, "the admin key's own organisation")
	assert.Equal(t, u.key, store.key)

	writes := f.writes
	for _, r := range Reconcile(ctx, c, spec, false) {
		assert.Equal(t, report.ActionOK, r.Action, "%s %s: %s", r.Kind, r.Name, r.Detail)
	}
	assert.Equal(t, writes, f.writes, "second run must not write")
}

func TestStoredAdminKeyIsReplaced(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFakeMISP()
	f.users = append(f.users, &fakeUser{id: 20, org: 1, role: 6, email: exportEmail, key: "ro-key"})
	c := newClient(t, f)
	// The admin key in the read-only Secret: it works, but not as this user.
	store := &memKeyStore{key: adminKey}
	spec := Spec{Users: []User{{Email: exportEmail, Role: readOnly, Key: store}}}

	dry := actions(Reconcile(ctx, c, spec, true))
	assert.Equal(t, report.ActionUpdate, dry["user-key/"+exportEmail])
	assert.Equal(t, 0, f.resets)

	got := actions(Reconcile(ctx, c, spec, false))
	assert.Equal(t, report.ActionUpdate, got["user-key/"+exportEmail])
	assert.Equal(t, "reset-1", store.key)
	assert.Equal(t, "reset-1", f.byEmail(exportEmail).key)
}

func TestRoleRestoredAndErrors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFakeMISP()
	f.users = append(f.users,
		&fakeUser{id: 20, org: 1, role: 3, email: exportEmail, key: "k20"},
		&fakeUser{id: 21, org: 1, role: 6, email: "off@example.com", key: "k21", disabled: true},
	)
	c := newClient(t, f)
	spec := Spec{Users: []User{
		{Email: exportEmail, Role: readOnly, Key: &memKeyStore{key: "k20"}},
		{Email: "off@example.com", Role: readOnly},
		{Email: "x@example.com", Role: "Nope"},
		{Email: "y@example.com", Role: readOnly, Org: "Missing"},
		{Email: "z@example.com", Role: readOnly, Org: orgOther},
	}}
	got := actions(Reconcile(ctx, c, spec, false))
	assert.Equal(t, report.ActionUpdate, got["user/"+exportEmail])
	assert.Equal(t, 6, f.byEmail(exportEmail).role)
	assert.Equal(t, report.ActionOK, got["user-key/"+exportEmail])
	assert.Equal(t, report.ActionError, got["user/off@example.com"])
	assert.Equal(t, report.ActionError, got["user/x@example.com"])
	assert.Equal(t, report.ActionError, got["user/y@example.com"])
	assert.Equal(t, report.ActionCreate, got["user/z@example.com"])
	assert.Equal(t, 2, f.byEmail("z@example.com").org)
}

func TestReconcileBadAdminKey(t *testing.T) {
	t.Parallel()
	c := newClient(t, newFakeMISP())
	c.APIKey = "wrong"
	got := Reconcile(context.Background(), c, Spec{Users: []User{{Email: exportEmail, Role: readOnly}}}, true)
	require.Len(t, got, 1)
	assert.Equal(t, report.ActionError, got[0].Action)
	assert.Contains(t, got[0].Detail, "HTTP 403")
}
