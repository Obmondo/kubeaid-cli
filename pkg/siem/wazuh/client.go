// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

// Package wazuh reconciles the RBAC of the per-tenant Wazuh manager
// APIs: rules mapping Keycloak realm roles (OpenSearch backend roles)
// of the SOC operators and of the manager's tenant to existing API
// roles. Each manager belongs to one tenant, so there are no per-group
// policies or agent groups. Users, agents, roles and policies are never
// modified; rule links are only added.
package wazuh

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

const maxErrorBody = 300

// Client is a Wazuh API client authenticating with basic credentials
// and a JWT, re-authenticating once on 401.
type Client struct {
	BaseURL  string
	Username string
	Password string
	HTTP     *http.Client

	mu    sync.Mutex
	token string
}

// APIError is a failed Wazuh API call.
type APIError struct {
	Method, Path string
	Code         int
	Message      string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("wazuh %s %s: HTTP %d: %s", e.Method, e.Path, e.Code, e.Message)
}

// response is the Wazuh API envelope.
type response struct {
	Data    json.RawMessage `json:"data"`
	Error   int             `json:"error"`
	Message string          `json:"message"`
}

// itemsData is data for list and write endpoints.
type itemsData struct {
	AffectedItems []json.RawMessage `json:"affected_items"`
	FailedItems   []json.RawMessage `json:"failed_items"`
}

func (c *Client) authenticate(ctx context.Context) error {
	url := strings.TrimRight(c.BaseURL, "/") + "/security/user/authenticate?raw=true"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return fmt.Errorf("building wazuh login: %w", err)
	}
	req.SetBasicAuth(c.Username, c.Password)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("wazuh login: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return fmt.Errorf("reading wazuh login: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return &APIError{Method: http.MethodPost, Path: "/security/user/authenticate", Code: resp.StatusCode, Message: "login refused"}
	}
	c.mu.Lock()
	c.token = strings.TrimSpace(string(raw))
	c.mu.Unlock()
	return nil
}

// call sends a request and decodes response.data into out.
func (c *Client) call(ctx context.Context, method, path string, in, out any) error {
	c.mu.Lock()
	haveToken := c.token != ""
	c.mu.Unlock()
	if !haveToken {
		if err := c.authenticate(ctx); err != nil {
			return err
		}
	}
	code, raw, err := c.send(ctx, method, path, in)
	if err == nil && code == http.StatusUnauthorized {
		if err := c.authenticate(ctx); err != nil {
			return err
		}
		code, raw, err = c.send(ctx, method, path, in)
	}
	if err != nil {
		return err
	}
	var env response
	_ = json.Unmarshal(raw, &env)
	if code < 200 || code > 299 || env.Error != 0 {
		msg := env.Message
		if msg == "" {
			msg = strings.TrimSpace(string(raw))
		}
		if len(msg) > maxErrorBody {
			msg = msg[:maxErrorBody]
		}
		return &APIError{Method: method, Path: path, Code: code, Message: msg}
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(env.Data, out); err != nil {
		return fmt.Errorf("decoding wazuh %s: %w", path, err)
	}
	return nil
}

func (c *Client) send(ctx context.Context, method, path string, in any) (int, []byte, error) {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return 0, nil, fmt.Errorf("encoding wazuh request: %w", err)
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+path, body)
	if err != nil {
		return 0, nil, fmt.Errorf("building wazuh request: %w", err)
	}
	c.mu.Lock()
	req.Header.Set("Authorization", "Bearer "+c.token)
	c.mu.Unlock()
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("wazuh %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, fmt.Errorf("reading wazuh %s: %w", path, err)
	}
	return resp.StatusCode, raw, nil
}

// items GETs a list endpoint and decodes its affected_items.
func items[T any](ctx context.Context, c *Client, path string) ([]T, error) {
	var data itemsData
	if err := c.call(ctx, http.MethodGet, path, nil, &data); err != nil {
		return nil, err
	}
	out := make([]T, 0, len(data.AffectedItems))
	for _, raw := range data.AffectedItems {
		var v T
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, fmt.Errorf("decoding wazuh %s item: %w", path, err)
		}
		out = append(out, v)
	}
	return out, nil
}

// createdID POSTs and returns the id of the first affected item.
func (c *Client) createdID(ctx context.Context, path string, in any) (int, error) {
	var data itemsData
	if err := c.call(ctx, http.MethodPost, path, in, &data); err != nil {
		return 0, err
	}
	if len(data.AffectedItems) == 0 {
		return 0, &APIError{Method: http.MethodPost, Path: path, Code: http.StatusOK, Message: "nothing created"}
	}
	var item struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal(data.AffectedItems[0], &item); err != nil {
		return 0, fmt.Errorf("decoding created item: %w", err)
	}
	return item.ID, nil
}
