// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

// Package indexer keeps the index lifecycle (ISM) retention policy on a
// Wazuh indexer (OpenSearch) and reads its health: cluster status and
// disk usage.
package indexer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const maxErrorBody = 300

// Client is an OpenSearch REST client with basic authentication.
type Client struct {
	BaseURL  string
	Username string
	Password string
	HTTP     *http.Client
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

// IsNotFound reports whether err is an HTTP 404 from OpenSearch.
func IsNotFound(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Code == http.StatusNotFound
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
