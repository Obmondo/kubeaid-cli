// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

// Package iris reconciles DFIR-IRIS customers (one per tenant) and
// the customer and group lists of IRIS service accounts. It never
// creates, deactivates or edits IRIS users: the Keycloak sync job owns
// human users.
package iris

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/Obmondo/kubeaid-cli/pkg/siem/report"
)

// Component is the report component name.
const Component = "iris"

const maxErrorBody = 300

// Client talks to the IRIS REST API with a bearer API key.
type Client struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
}

// APIError is a non-success IRIS answer.
type APIError struct {
	Path    string
	Code    int
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("IRIS %s: HTTP %d: %s", e.Path, e.Code, e.Message)
}

// envelope is IRIS's {"status", "message", "data"} response wrapper.
type envelope struct {
	Status  string          `json:"status"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func (c *Client) call(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("encoding IRIS request: %w", err)
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+path, body)
	if err != nil {
		return fmt.Errorf("building IRIS request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("IRIS %s: %w", path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading IRIS %s: %w", path, err)
	}
	var env envelope
	_ = json.Unmarshal(raw, &env)
	if resp.StatusCode < 200 || resp.StatusCode > 299 || (env.Status != "" && env.Status != "success") {
		msg := env.Message
		if msg == "" {
			msg = strings.TrimSpace(string(raw))
			if len(msg) > maxErrorBody {
				msg = msg[:maxErrorBody]
			}
		}
		return &APIError{Path: path, Code: resp.StatusCode, Message: msg}
	}
	if out == nil || len(env.Data) == 0 {
		return nil
	}
	if err := json.Unmarshal(env.Data, out); err != nil {
		return fmt.Errorf("decoding IRIS %s: %w", path, err)
	}
	return nil
}

// Customer is a desired IRIS customer.
type Customer struct {
	Name        string
	Description string
}

// ServiceAccount is an IRIS login whose groups and customers are managed
// (added to, never removed from). With Create it is added as an IRIS
// service account when missing; with Key its API key is kept there.
type ServiceAccount struct {
	Login  string
	Groups []string
	Create bool
	Name   string
	Email  string
	Key    KeyStore
}

// Spec is the desired IRIS state.
type Spec struct {
	Customers []Customer
	// InitialCustomer is added to every service account's customers.
	InitialCustomer string
	ServiceAccounts []ServiceAccount
}

// Reconcile brings IRIS in line with spec. Errors are reported as
// results; the run continues with the next object.
func Reconcile(ctx context.Context, c *Client, spec Spec, dryRun bool) []report.Result {
	var results []report.Result
	add := func(kind, name string, a report.Action, detail string) {
		results = append(results, report.Result{Component: Component, Kind: kind, Name: name, Action: a, Detail: detail})
	}

	customers, err := c.customers(ctx)
	if err != nil {
		add("api", "customers", report.ActionError, err.Error())
		return results
	}
	for _, want := range spec.Customers {
		if _, ok := customers[want.Name]; ok {
			add("customer", want.Name, report.ActionOK, "")
			continue
		}
		if dryRun {
			add("customer", want.Name, report.ActionCreate, "")
			continue
		}
		body := map[string]any{"customer_name": want.Name, "customer_description": want.Description, "customer_sla": ""}
		if err := c.call(ctx, http.MethodPost, "/manage/customers/add", body, nil); err != nil {
			add("customer", want.Name, report.ActionError, err.Error())
			continue
		}
		add("customer", want.Name, report.ActionCreate, "")
	}
	if len(spec.ServiceAccounts) == 0 {
		return results
	}
	if !dryRun {
		// Pick up the ids of customers created above.
		if customers, err = c.customers(ctx); err != nil {
			add("api", "customers", report.ActionError, err.Error())
			return results
		}
	}
	groups, err := c.groups(ctx)
	if err != nil {
		add("api", "groups", report.ActionError, err.Error())
		return results
	}

	wantCustomers := make([]string, 0, len(spec.Customers)+1)
	if spec.InitialCustomer != "" {
		wantCustomers = append(wantCustomers, spec.InitialCustomer)
	}
	for _, cu := range spec.Customers {
		wantCustomers = append(wantCustomers, cu.Name)
	}
	for _, sa := range spec.ServiceAccounts {
		results = append(results, c.reconcileServiceAccount(ctx, sa, groups, customers, wantCustomers, dryRun)...)
	}
	return results
}

func (c *Client) reconcileServiceAccount(
	ctx context.Context, sa ServiceAccount, groups, customers map[string]int, wantCustomers []string, dryRun bool,
) (out []report.Result) {
	res := func(kind string, a report.Action, detail string) report.Result {
		return report.Result{Component: Component, Kind: kind, Name: sa.Login, Action: a, Detail: detail}
	}
	var createdKey string
	lookup, found, err := c.lookupLogin(ctx, sa.Login)
	switch {
	case err != nil:
		return []report.Result{res("service-account", report.ActionError, err.Error())}
	case !found && !sa.Create:
		return []report.Result{res("service-account", report.ActionSkip, "login not found in IRIS; create it there first")}
	case !found && dryRun:
		out = append(out, res("service-account", report.ActionCreate, "service account"))
		if sa.Key != nil {
			out = append(out, res("service-account-key", report.ActionCreate, "would store the new account's key"))
		}
		return out
	case !found:
		if createdKey, err = c.createServiceAccount(ctx, sa); err != nil {
			return []report.Result{res("service-account", report.ActionError, err.Error())}
		}
		out = append(out, res("service-account", report.ActionCreate, "service account"))
		if lookup, found, err = c.lookupLogin(ctx, sa.Login); err != nil || !found {
			if err == nil {
				err = errors.New("created but not found")
			}
			return append(out, res("service-account", report.ActionError, err.Error()))
		}
	}
	uid := pickInt(lookup, "user_id", "id")
	var user map[string]any
	if err := c.call(ctx, http.MethodGet, "/manage/users/"+strconv.Itoa(uid), nil, &user); err != nil {
		return []report.Result{res("service-account", report.ActionError, err.Error())}
	}

	if sa.Key != nil {
		defer func() { out = append(out, c.ensureKey(ctx, sa, uid, createdKey, dryRun)) }()
	}

	curGroups := idSet(user["user_groups"], "group_id", "id")
	gIDs, gMissing, unknown := resolve(sa.Groups, groups, curGroups)
	switch {
	case len(unknown) > 0:
		out = append(out, res("service-account-groups", report.ActionError, "IRIS groups do not exist: "+strings.Join(unknown, ",")))
	case len(gMissing) > 0:
		out = append(out, c.update(ctx, res, "service-account-groups", uid, "groups", gIDs, gMissing, dryRun))
	default:
		out = append(out, res("service-account-groups", report.ActionOK, ""))
	}

	curCustomers := idSet(user["user_customers"], "customer_id", "client_id", "id")
	cIDs, cMissing, unknownCust := resolve(wantCustomers, customers, curCustomers)
	cMissing = append(cMissing, unknownCust...) // not created yet (dry run)
	if len(unknownCust) > 0 && !dryRun {
		out = append(out, res("service-account-customers", report.ActionError, "IRIS customers do not exist: "+strings.Join(unknownCust, ",")))
		return out
	}
	if len(cMissing) == 0 {
		return append(out, res("service-account-customers", report.ActionOK, ""))
	}
	return append(out, c.update(ctx, res, "service-account-customers", uid, "customers", cIDs, cMissing, dryRun))
}

// update POSTs the union of current and wanted ids. IRIS replaces the
// membership list, so the current entries are always sent along.
func (c *Client) update(
	ctx context.Context, res func(string, report.Action, string) report.Result,
	kind string, uid int, what string, ids, added []string, dryRun bool,
) report.Result {
	detail := "add " + strings.Join(added, ",")
	if dryRun {
		return res(kind, report.ActionUpdate, detail)
	}
	nums := make([]int, 0, len(ids))
	for _, s := range ids {
		n, err := strconv.Atoi(s)
		if err != nil {
			return res(kind, report.ActionError, "bad id "+s)
		}
		nums = append(nums, n)
	}
	sort.Ints(nums)
	body := map[string]any{what + "_membership": nums}
	path := fmt.Sprintf("/manage/users/%d/%s/update", uid, what)
	if err := c.call(ctx, http.MethodPost, path, body, nil); err != nil {
		return res(kind, report.ActionError, err.Error())
	}
	return res(kind, report.ActionUpdate, detail)
}

// resolve maps wanted names to ids and returns the full id list
// (current plus wanted), the wanted names not yet present, and the
// wanted names IRIS does not know.
func resolve(want []string, byName map[string]int, current map[string]bool) (ids, missingNames, unknown []string) {
	all := map[string]bool{}
	for id := range current {
		all[id] = true
	}
	for _, name := range want {
		id, ok := byName[name]
		if !ok {
			unknown = append(unknown, name)
			continue
		}
		key := strconv.Itoa(id)
		if !current[key] {
			missingNames = append(missingNames, name)
		}
		all[key] = true
	}
	for id := range all {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, missingNames, unknown
}

func (c *Client) customers(ctx context.Context) (map[string]int, error) {
	var list []map[string]any
	if err := c.call(ctx, http.MethodGet, "/manage/customers/list", nil, &list); err != nil {
		return nil, err
	}
	out := make(map[string]int, len(list))
	for _, cu := range list {
		out[pickString(cu, "customer_name", "name")] = pickInt(cu, "customer_id", "client_id", "id")
	}
	return out, nil
}

func (c *Client) groups(ctx context.Context) (map[string]int, error) {
	var list []map[string]any
	if err := c.call(ctx, http.MethodGet, "/manage/groups/list", nil, &list); err != nil {
		return nil, err
	}
	out := make(map[string]int, len(list))
	for _, g := range list {
		out[pickString(g, "group_name", "name")] = pickInt(g, "group_id", "id")
	}
	return out, nil
}

func pickString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := m[k].(string); ok {
			return s
		}
	}
	return ""
}

func pickInt(m map[string]any, keys ...string) int {
	for _, k := range keys {
		if f, ok := m[k].(float64); ok {
			return int(f)
		}
	}
	return 0
}

func idSet(v any, keys ...string) map[string]bool {
	out := map[string]bool{}
	list, ok := v.([]any)
	if !ok {
		return out
	}
	for _, item := range list {
		switch t := item.(type) {
		case map[string]any:
			out[strconv.Itoa(pickInt(t, keys...))] = true
		case float64:
			out[strconv.Itoa(int(t))] = true
		}
	}
	return out
}
