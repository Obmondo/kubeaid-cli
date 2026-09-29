// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

// Package wazuhcentral reconciles the central search-only OpenSearch
// indexer: the cross-cluster search connections to the tenants'
// indexers (persistent cluster.remote.<alias>.seeds settings), and the
// Wazuh dashboard app config Secret listing every tenant manager.
// Remotes not in the config are reported, never removed.
package wazuhcentral

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const (
	maxErrorBody  = 300
	keyPersistent = "persistent"
)

// Client is an OpenSearch REST client with basic authentication.
type Client struct {
	BaseURL  string
	Username string
	Password string
	HTTP     *http.Client
	// headers are sent on every request (the dashboard's osd-xsrf).
	headers map[string]string
}

// APIError is a failed OpenSearch call.
type APIError struct {
	Method, Path string
	Code         int
	Message      string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("opensearch %s %s: HTTP %d: %s", e.Method, e.Path, e.Code, e.Message)
}

// call sends in as JSON and decodes the response into out.
func (c *Client) call(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("encoding opensearch request: %w", err)
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+path, body)
	if err != nil {
		return fmt.Errorf("building opensearch request: %w", err)
	}
	req.SetBasicAuth(c.Username, c.Password)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("opensearch %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading opensearch %s: %w", path, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		msg := strings.TrimSpace(string(raw))
		if len(msg) > maxErrorBody {
			msg = msg[:maxErrorBody]
		}
		return &APIError{Method: method, Path: path, Code: resp.StatusCode, Message: msg}
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decoding opensearch %s: %w", path, err)
	}
	return nil
}

// persistentSettings returns the flat persistent cluster settings.
func (c *Client) persistentSettings(ctx context.Context) (map[string]json.RawMessage, error) {
	var out struct {
		Persistent map[string]json.RawMessage `json:"persistent"`
	}
	if err := c.call(ctx, http.MethodGet, "/_cluster/settings?flat_settings=true", nil, &out); err != nil {
		return nil, err
	}
	return out.Persistent, nil
}

// putPersistent sets persistent cluster settings.
func (c *Client) putPersistent(ctx context.Context, settings map[string]any) error {
	var out struct {
		Acknowledged bool `json:"acknowledged"`
	}
	if err := c.call(ctx, http.MethodPut, "/_cluster/settings", map[string]any{keyPersistent: settings}, &out); err != nil {
		return err
	}
	if !out.Acknowledged {
		return &APIError{Method: http.MethodPut, Path: "/_cluster/settings", Code: http.StatusOK, Message: "not acknowledged"}
	}
	return nil
}
