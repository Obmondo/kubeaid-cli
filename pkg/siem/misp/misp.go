// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

// Package misp keeps API-only MISP users (for example the read-only
// user of the IOC export) and their auth keys: a user is created when
// missing and kept in its role, and a working auth key of it is kept in
// a Kubernetes Secret, so jobs that only read MISP need not hold the
// site admin's key. Users are never deleted or disabled.
package misp

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/Obmondo/kubeaid-cli/pkg/siem/report"
)

// Component is the report component name.
const Component = "misp"

const (
	maxErrorBody = 300
	fieldRoleID  = "role_id"
	fieldName    = "name"
)

// Client talks to the MISP REST API with an auth key.
type Client struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
}

// APIError is a non-success MISP answer.
type APIError struct {
	Path    string
	Code    int
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("MISP %s: HTTP %d: %s", e.Path, e.Code, e.Message)
}

func (c *Client) call(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("encoding MISP request: %w", err)
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+path, body)
	if err != nil {
		return fmt.Errorf("building MISP request: %w", err)
	}
	req.Header.Set("Authorization", c.APIKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("MISP %s: %w", path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading MISP %s: %w", path, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		msg := strings.TrimSpace(string(raw))
		if len(msg) > maxErrorBody {
			msg = msg[:maxErrorBody]
		}
		return &APIError{Path: path, Code: resp.StatusCode, Message: msg}
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decoding MISP %s: %w", path, err)
	}
	return nil
}

// KeyStore holds a user's auth key outside MISP (a Secret key).
type KeyStore interface {
	// Get returns the stored key, "" when there is none yet.
	Get(ctx context.Context) (string, error)
	// Put stores key; dry-run aware.
	Put(ctx context.Context, key string, dryRun bool) (report.Action, error)
}

// User is a desired MISP user.
type User struct {
	Email string
	// Role is the role name, e.g. "Read Only".
	Role string
	// Org is the organisation name; "" is the admin key's own org.
	Org string
	Key KeyStore
}

// Spec is the desired MISP state.
type Spec struct {
	Users []User
}

// Reconcile brings MISP in line with spec. Errors are reported as
// results; the run continues with the next user.
func Reconcile(ctx context.Context, c *Client, spec Spec, dryRun bool) []report.Result {
	if len(spec.Users) == 0 {
		return nil
	}
	apiErr := func(name string, err error) []report.Result {
		return []report.Result{{Component: Component, Kind: "api", Name: name, Action: report.ActionError, Detail: err.Error()}}
	}
	me, err := c.whoami(ctx)
	if err != nil {
		return apiErr("users/view/me", err)
	}
	roles, err := c.roles(ctx)
	if err != nil {
		return apiErr("roles", err)
	}
	users, err := c.users(ctx)
	if err != nil {
		return apiErr("users", err)
	}
	var orgs map[string]int
	var results []report.Result
	for _, u := range spec.Users {
		orgID := me.OrgID
		if u.Org != "" {
			if orgs == nil {
				if orgs, err = c.orgs(ctx); err != nil {
					return append(results, apiErr("organisations", err)...)
				}
			}
			id, ok := orgs[u.Org]
			if !ok {
				results = append(results, report.Result{Component: Component, Kind: "user", Name: u.Email, Action: report.ActionError, Detail: "MISP organisation " + u.Org + " does not exist"})
				continue
			}
			orgID = id
		}
		results = append(results, c.reconcileUser(ctx, u, roles, users, orgID, dryRun)...)
	}
	return results
}

// account is the part of a MISP user the reconciler looks at.
type account struct {
	ID       int
	Email    string
	OrgID    int
	RoleID   int
	Disabled bool
}

func (c *Client) reconcileUser(ctx context.Context, u User, roles map[string]int, users map[string]account, orgID int, dryRun bool) []report.Result {
	res := func(kind string, a report.Action, detail string) report.Result {
		return report.Result{Component: Component, Kind: kind, Name: u.Email, Action: a, Detail: detail}
	}
	roleID, ok := roles[u.Role]
	if !ok {
		return []report.Result{res("user", report.ActionError, "MISP role "+u.Role+" does not exist")}
	}
	cur, found := users[strings.ToLower(u.Email)]
	var out []report.Result
	var createdKey string
	switch {
	case !found && dryRun:
		out = append(out, res("user", report.ActionCreate, "role "+u.Role))
		if u.Key != nil {
			out = append(out, res("user-key", report.ActionCreate, "would store the new user's key"))
		}
		return out
	case !found:
		created, key, err := c.createUser(ctx, u.Email, orgID, roleID)
		if err != nil {
			return []report.Result{res("user", report.ActionError, err.Error())}
		}
		cur, createdKey = created, key
		out = append(out, res("user", report.ActionCreate, "role "+u.Role))
	case cur.Disabled:
		return []report.Result{res("user", report.ActionError, "user is disabled in MISP; enable it or remove it from the config")}
	case cur.OrgID != orgID:
		return []report.Result{res("user", report.ActionError, fmt.Sprintf("user is in organisation %d, want %d; fix it in MISP", cur.OrgID, orgID))}
	case cur.RoleID != roleID:
		detail := fmt.Sprintf("role %d -> %s (%d)", cur.RoleID, u.Role, roleID)
		if !dryRun {
			if err := c.call(ctx, http.MethodPost, "/admin/users/edit/"+strconv.Itoa(cur.ID), map[string]any{fieldRoleID: roleID}, nil); err != nil {
				return []report.Result{res("user", report.ActionError, "setting the role: "+err.Error())}
			}
		}
		out = append(out, res("user", report.ActionUpdate, detail))
	default:
		out = append(out, res("user", report.ActionOK, ""))
	}
	if u.Key != nil {
		out = append(out, c.ensureKey(ctx, u, cur.ID, createdKey, dryRun))
	}
	return out
}

// ensureKey keeps a working auth key of the user in u.Key: a stored key
// that MISP accepts as this user is left alone, otherwise the new user's
// key, or a freshly generated one, is stored.
func (c *Client) ensureKey(ctx context.Context, u User, uid int, createdKey string, dryRun bool) report.Result {
	res := func(a report.Action, detail string) report.Result {
		return report.Result{Component: Component, Kind: "user-key", Name: u.Email, Action: a, Detail: detail}
	}
	stored, err := u.Key.Get(ctx)
	if err != nil {
		return res(report.ActionError, err.Error())
	}
	if stored != "" && createdKey == "" {
		ok, err := c.keyIsUser(ctx, stored, uid)
		if err != nil {
			return res(report.ActionError, err.Error())
		}
		if ok {
			return res(report.ActionOK, "")
		}
	}
	key, detail := createdKey, "stored the new user's key"
	if key == "" {
		if dryRun {
			return res(report.ActionUpdate, "would generate a new auth key and store it")
		}
		if key, err = c.resetKey(ctx, uid); err != nil {
			return res(report.ActionError, "generating an auth key: "+err.Error())
		}
		detail = "generated a new auth key and stored it"
	}
	action, err := u.Key.Put(ctx, key, dryRun)
	if err != nil {
		return res(report.ActionError, err.Error())
	}
	if action == report.ActionOK {
		return res(report.ActionOK, "")
	}
	return res(action, detail)
}

// keyIsUser tells whether MISP accepts key as the user uid (and not as
// anyone else, e.g. an admin key stored there by mistake).
func (c *Client) keyIsUser(ctx context.Context, key string, uid int) (bool, error) {
	probe := &Client{BaseURL: c.BaseURL, APIKey: key, HTTP: c.HTTP}
	me, err := probe.whoami(ctx)
	var apiErr *APIError
	switch {
	case err == nil:
		return me.ID == uid, nil
	case errors.As(err, &apiErr) && (apiErr.Code == http.StatusUnauthorized || apiErr.Code == http.StatusForbidden):
		return false, nil
	default:
		return false, err
	}
}

var newKeyPattern = regexp.MustCompile(`Authkey updated: (\S+)`)

// resetKey generates a new auth key for uid (both with and without
// Security.advanced_authkeys) and returns it.
func (c *Client) resetKey(ctx context.Context, uid int) (string, error) {
	var out map[string]any
	if err := c.call(ctx, http.MethodPost, "/users/resetauthkey/"+strconv.Itoa(uid), map[string]any{}, &out); err != nil {
		return "", err
	}
	for _, k := range []string{"message", fieldName} {
		if s, ok := out[k].(string); ok {
			if m := newKeyPattern.FindStringSubmatch(s); m != nil {
				return m[1], nil
			}
		}
	}
	return "", errors.New("MISP reset the key but did not return it")
}

// createUser adds an API-only user (random password nobody keeps, no
// e-mail sent) and returns it with its auth key.
func (c *Client) createUser(ctx context.Context, email string, orgID, roleID int) (account, string, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return account{}, "", err
	}
	body := map[string]any{
		"email":         email,
		"org_id":        orgID,
		fieldRoleID:     roleID,
		"password":      base64.RawURLEncoding.EncodeToString(secret) + "Aa1!",
		"change_pw":     0,
		"termsaccepted": 1,
		"notify":        0,
		"autoalert":     0,
		"contactalert":  0,
	}
	var out map[string]any
	if err := c.call(ctx, http.MethodPost, "/admin/users/add", body, &out); err != nil {
		return account{}, "", err
	}
	u := unwrap(out, "User")
	a := toAccount(u)
	if a.ID == 0 {
		return account{}, "", errors.New("MISP created the user but returned no id")
	}
	return a, pickString(u, "authkey"), nil
}

func (c *Client) whoami(ctx context.Context) (account, error) {
	var out map[string]any
	if err := c.call(ctx, http.MethodGet, "/users/view/me", nil, &out); err != nil {
		return account{}, err
	}
	return toAccount(unwrap(out, "User")), nil
}

func (c *Client) roles(ctx context.Context) (map[string]int, error) {
	return c.index(ctx, "/roles/index", "Role")
}

func (c *Client) orgs(ctx context.Context) (map[string]int, error) {
	return c.index(ctx, "/organisations/index", "Organisation")
}

// index lists name -> id of a MISP index endpoint; rows come as
// {"<Model>": {...}} or flat.
func (c *Client) index(ctx context.Context, path, model string) (map[string]int, error) {
	var list []map[string]any
	if err := c.call(ctx, http.MethodGet, path, nil, &list); err != nil {
		return nil, err
	}
	out := make(map[string]int, len(list))
	for _, row := range list {
		r := unwrap(row, model)
		out[pickString(r, fieldName)] = pickInt(r, "id")
	}
	return out, nil
}

// users lists the MISP users by lower-cased e-mail.
func (c *Client) users(ctx context.Context) (map[string]account, error) {
	var list []map[string]any
	if err := c.call(ctx, http.MethodGet, "/admin/users/index", nil, &list); err != nil {
		return nil, err
	}
	out := make(map[string]account, len(list))
	for _, row := range list {
		a := toAccount(unwrap(row, "User"))
		out[strings.ToLower(a.Email)] = a
	}
	return out, nil
}

func unwrap(m map[string]any, model string) map[string]any {
	if inner, ok := m[model].(map[string]any); ok {
		return inner
	}
	return m
}

func toAccount(u map[string]any) account {
	return account{
		ID: pickInt(u, "id"), Email: pickString(u, "email"), OrgID: pickInt(u, "org_id"),
		RoleID: pickInt(u, fieldRoleID), Disabled: pickBool(u, "disabled"),
	}
}

// pickInt reads MISP ids, which come as JSON strings or numbers.
func pickInt(m map[string]any, key string) int {
	switch v := m[key].(type) {
	case float64:
		return int(v)
	case string:
		n, _ := strconv.Atoi(v)
		return n
	}
	return 0
}

func pickString(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

func pickBool(m map[string]any, key string) bool {
	switch v := m[key].(type) {
	case bool:
		return v
	case string:
		return v == "1" || v == "true"
	case float64:
		return v != 0
	}
	return false
}
