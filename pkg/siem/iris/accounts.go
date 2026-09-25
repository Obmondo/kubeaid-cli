// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package iris

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/Obmondo/kubeaid-cli/pkg/siem/report"
)

// KeyStore holds a service account's IRIS API key outside IRIS (a
// Kubernetes Secret key), for the job that logs in with it.
type KeyStore interface {
	// Get returns the stored key, "" when there is none yet.
	Get(ctx context.Context) (string, error)
	// Put stores key; dry-run aware.
	Put(ctx context.Context, key string, dryRun bool) (report.Action, error)
}

// createServiceAccount adds sa as an IRIS service account (no password,
// not an administrator) and returns its API key.
func (c *Client) createServiceAccount(ctx context.Context, sa ServiceAccount) (string, error) {
	name := sa.Name
	if name == "" {
		name = sa.Login
	}
	email := sa.Email
	if email == "" {
		email = sa.Login + "@service.invalid"
	}
	body := map[string]any{
		"user_login":              sa.Login,
		"user_name":               name,
		"user_email":              email,
		"user_isadmin":            false,
		"user_is_service_account": true,
		"active":                  true,
	}
	var out map[string]any
	if err := c.call(ctx, http.MethodPost, "/manage/users/add", body, &out); err != nil {
		return "", err
	}
	return pickString(out, "user_api_key", "api_key"), nil
}

// keyWorks tells whether IRIS accepts key.
func (c *Client) keyWorks(ctx context.Context, key string) (bool, error) {
	probe := &Client{BaseURL: c.BaseURL, APIKey: key, HTTP: c.HTTP}
	err := probe.call(ctx, http.MethodGet, "/api/ping", nil, nil)
	var apiErr *APIError
	switch {
	case err == nil:
		return true, nil
	case errors.As(err, &apiErr) && (apiErr.Code == http.StatusUnauthorized || apiErr.Code == http.StatusForbidden):
		return false, nil
	default:
		return false, err
	}
}

// ensureKey keeps a working API key of the account in sa.Key: a stored key
// IRIS accepts is left alone; otherwise the key from a just-created account,
// or a renewed one, is stored.
func (c *Client) ensureKey(ctx context.Context, sa ServiceAccount, uid int, createdKey string, dryRun bool) report.Result {
	res := func(a report.Action, detail string) report.Result {
		return report.Result{Component: Component, Kind: "service-account-key", Name: sa.Login, Action: a, Detail: detail}
	}
	stored, err := sa.Key.Get(ctx)
	if err != nil {
		return res(report.ActionError, err.Error())
	}
	if stored != "" {
		ok, err := c.keyWorks(ctx, stored)
		if err != nil {
			return res(report.ActionError, err.Error())
		}
		if ok {
			return res(report.ActionOK, "")
		}
	}
	key, detail := createdKey, "stored the new account's key"
	if key == "" {
		if dryRun {
			return res(report.ActionUpdate, "would renew the API key and store it")
		}
		var out map[string]any
		if err := c.call(ctx, http.MethodPost, "/manage/users/renew-api-key/"+strconv.Itoa(uid), nil, &out); err != nil {
			return res(report.ActionError, "renewing the API key: "+err.Error())
		}
		if key = pickString(out, "user_api_key", "api_key"); key == "" {
			return res(report.ActionError, "IRIS renewed the key but did not return it")
		}
		detail = "renewed the API key and stored it"
	}
	action, err := sa.Key.Put(ctx, key, dryRun)
	if err != nil {
		return res(report.ActionError, err.Error())
	}
	if action == report.ActionOK {
		return res(report.ActionOK, "")
	}
	return res(action, detail)
}

// lookupLogin finds an IRIS user by login; found is false when IRIS has no
// such login.
func (c *Client) lookupLogin(ctx context.Context, login string) (map[string]any, bool, error) {
	var lookup map[string]any
	err := c.call(ctx, http.MethodGet, "/manage/users/lookup/login/"+url.PathEscape(login), nil, &lookup)
	var apiErr *APIError
	switch {
	case err == nil:
		return lookup, true, nil
	case errors.As(err, &apiErr) && (apiErr.Code == http.StatusNotFound || apiErr.Code == http.StatusBadRequest):
		return nil, false, nil
	default:
		return nil, false, err
	}
}
