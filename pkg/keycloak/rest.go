// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package keycloak

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// APIError is returned by the raw admin-API helper for non-2xx
// responses. Body is truncated and never contains request data.
type APIError struct {
	Method string
	Path   string
	Code   int
	Body   string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("%s %s: HTTP %d: %s", e.Method, e.Path, e.Code, e.Body)
}

// isRawNotFound reports whether err is a 404 from the raw helper.
func isRawNotFound(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Code == http.StatusNotFound
}

// JSON keys and values repeated across the raw admin-API code.
const (
	keyName      = "name"
	keyAlias     = "alias"
	keyRealm     = "realm"
	keyConfig    = "config"
	keyEnabled   = "enabled"
	protocolOIDC = "openid-connect"
	valueTrue    = "true"
	methodGet    = "GET"
	methodPost   = "POST"
	methodPut    = "PUT"
)

// maxErrorBody caps how much of an error response is kept.
const maxErrorBody = 300

// realmPath builds /admin/realms/{realm}{suffix}, escaping the realm.
func realmPath(realm, suffix string) string {
	return "/admin/realms/" + url.PathEscape(realm) + suffix
}

// do issues an authenticated admin-API request. in (if non-nil) is
// JSON-encoded as the body; out (if non-nil) receives the decoded
// response. A 401 triggers one re-login and retry: the admin token is
// short-lived and a long reconcile run outlives it.
func (r *Reconciler) do(ctx context.Context, method, path string, in, out any) error {
	var body []byte
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("encoding request body for %s %s: %w", method, path, err)
		}
		body = b
	}

	resp, err := r.send(ctx, method, path, body, r.tok(ctx))
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusUnauthorized && r.adminPassword != "" {
		_ = resp.Body.Close()
		if err := r.login(ctx); err != nil {
			return err
		}
		resp, err = r.send(ctx, method, path, body, r.tok(ctx))
		if err != nil {
			return err
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return &APIError{Method: method, Path: path, Code: resp.StatusCode, Body: strings.TrimSpace(string(raw))}
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response of %s %s: %w", method, path, err)
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decoding response of %s %s: %w", method, path, err)
	}
	return nil
}

func (r *Reconciler) send(ctx context.Context, method, path string, body []byte, token string) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(r.baseURL, "/")+path, reader)
	if err != nil {
		return nil, fmt.Errorf("building request %s %s: %w", method, path, err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", method, path, err)
	}
	return resp, nil
}
