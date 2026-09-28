// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package wazuh

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// File kinds of the ruleset file endpoints (/rules/files, /decoders/files,
// /lists/files). Uploads always land in the manager's user directories
// (etc/rules, etc/decoders, etc/lists).
const (
	KindRules    = "rules"
	KindDecoders = "decoders"
	KindLists    = "lists"
)

// Wazuh error codes of a file or group that does not exist.
const (
	codeRuleFileNotFound    = 1415
	codeDecoderFileNotFound = 1503
	codeListFileNotFound    = 1802
	codeGroupNotFound       = 1710
)

// ErrNotFound is returned for a ruleset file or agent group that does
// not exist.
var ErrNotFound = errors.New("not found")

// userDir is where the API writes a kind's files; reads are limited to
// it so a stock file of the same name is never mistaken for ours.
func userDir(kind string) string {
	switch kind {
	case KindRules:
		return "etc/rules"
	case KindDecoders:
		return "etc/decoders"
	default:
		return ""
	}
}

func filePath(kind, name string, query url.Values) string {
	p := "/" + kind + "/files/" + url.PathEscape(name)
	if len(query) > 0 {
		p += "?" + query.Encode()
	}
	return p
}

// failedItems is data of a partly failed call.
type failedItems struct {
	FailedItems []struct {
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	} `json:"failed_items"`
}

// firstFailure returns the code and message of the first failed item
// in a Wazuh envelope, or 0.
func firstFailure(raw []byte) (int, string) {
	var env struct {
		Data    failedItems `json:"data"`
		Error   int         `json:"error"`
		Message string      `json:"message"`
	}
	if json.Unmarshal(raw, &env) != nil {
		return 0, ""
	}
	if len(env.Data.FailedItems) > 0 {
		return env.Data.FailedItems[0].Error.Code, env.Data.FailedItems[0].Error.Message
	}
	return env.Error, env.Message
}

// GetFile returns the raw content of a user ruleset file, or
// ErrNotFound.
func (c *Client) GetFile(ctx context.Context, kind, name string) ([]byte, error) {
	q := url.Values{"raw": {"true"}}
	if d := userDir(kind); d != "" {
		q.Set("relative_dirname", d)
	}
	path := filePath(kind, name, q)
	code, ctype, raw, err := c.do(ctx, http.MethodGet, path, "application/json", nil)
	if err != nil {
		return nil, err
	}
	if code == http.StatusOK && !strings.HasPrefix(ctype, "application/json") {
		return raw, nil
	}
	fc, msg := firstFailure(raw)
	switch fc {
	case codeRuleFileNotFound, codeDecoderFileNotFound, codeListFileNotFound:
		return nil, ErrNotFound
	}
	if msg == "" {
		msg = truncate(string(raw))
	}
	return nil, &APIError{Method: http.MethodGet, Path: path, Code: code, Message: msg}
}

// PutFile uploads (overwrites) a user ruleset file. The API checks the
// XML before writing, so a malformed rule or decoder file is refused
// and the old one kept.
func (c *Client) PutFile(ctx context.Context, kind, name string, content []byte) error {
	return c.rawWrite(ctx, http.MethodPut, filePath(kind, name, url.Values{"overwrite": {"true"}}), "application/octet-stream", content)
}

// DeleteFile removes a user ruleset file; a missing file is not an
// error.
func (c *Client) DeleteFile(ctx context.Context, kind, name string) error {
	q := url.Values{}
	if d := userDir(kind); d != "" {
		q.Set("relative_dirname", d)
	}
	err := c.rawWrite(ctx, http.MethodDelete, filePath(kind, name, q), "application/json", nil)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}

// rawWrite sends a write and checks the envelope, mapping the
// not-found codes to ErrNotFound.
func (c *Client) rawWrite(ctx context.Context, method, path, contentType string, body []byte) error {
	code, _, raw, err := c.do(ctx, method, path, contentType, body)
	if err != nil {
		return err
	}
	fc, msg := firstFailure(raw)
	if code >= 200 && code <= 299 && fc == 0 {
		return nil
	}
	switch fc {
	case codeRuleFileNotFound, codeDecoderFileNotFound, codeListFileNotFound, codeGroupNotFound:
		return ErrNotFound
	}
	if msg == "" {
		msg = truncate(string(raw))
	}
	return &APIError{Method: method, Path: path, Code: code, Message: msg}
}

func truncate(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > maxErrorBody {
		return s[:maxErrorBody]
	}
	return s
}

// ValidateConfiguration runs GET /manager/configuration/validation and
// returns an error naming every node whose configuration is invalid.
func (c *Client) ValidateConfiguration(ctx context.Context) error {
	type node struct {
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	var data struct {
		AffectedItems []node `json:"affected_items"`
		FailedItems   []struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		} `json:"failed_items"`
	}
	if err := c.call(ctx, http.MethodGet, "/manager/configuration/validation", nil, &data); err != nil {
		return err
	}
	var bad []string
	for _, n := range data.AffectedItems {
		if !strings.EqualFold(n.Status, "OK") {
			bad = append(bad, n.Name+": "+n.Status)
		}
	}
	for _, f := range data.FailedItems {
		bad = append(bad, f.Error.Message)
	}
	if len(bad) > 0 {
		return fmt.Errorf("configuration validation failed: %s", strings.Join(bad, "; "))
	}
	return nil
}

// ReloadAnalysisd hot-reloads the ruleset (rules, decoders, CDB lists)
// on every cluster node, or on the manager when it runs without a
// cluster, and returns the warnings analysisd printed while loading,
// one per entry, prefixed with the node name.
func (c *Client) ReloadAnalysisd(ctx context.Context) ([]string, error) {
	type node struct {
		Name string `json:"name"`
		Msg  string `json:"msg"`
	}
	var data struct {
		AffectedItems []node `json:"affected_items"`
		FailedItems   []struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		} `json:"failed_items"`
	}
	err := c.call(ctx, http.MethodPut, "/cluster/analysisd/reload", nil, &data)
	var apiErr *APIError
	if errors.As(err, &apiErr) && strings.Contains(apiErr.Message, "Cluster is not running") {
		err = c.call(ctx, http.MethodPut, "/manager/analysisd/reload", nil, &data)
	}
	if err != nil {
		return nil, err
	}
	if len(data.FailedItems) > 0 {
		return nil, fmt.Errorf("analysisd reload failed: %s", data.FailedItems[0].Error.Message)
	}
	var warnings []string
	for _, n := range data.AffectedItems {
		for _, w := range SplitReloadMessage(n.Msg) {
			warnings = append(warnings, n.Name+": "+w)
		}
	}
	return warnings, nil
}

// reloadOK is the message of a reload without warnings.
const reloadOK = "Ruleset reload request sent successfully."

// SplitReloadMessage splits a reload msg ("(7617): ...., (7619): ...")
// into its warnings; the success message yields none.
func SplitReloadMessage(msg string) []string {
	msg = strings.TrimSpace(msg)
	if msg == "" || msg == reloadOK {
		return nil
	}
	parts := strings.Split(msg, ", (")
	out := make([]string, 0, len(parts))
	for i, p := range parts {
		if i > 0 {
			p = "(" + p
		}
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// RegisteredLists returns the CDB list paths in the manager's <ruleset>
// (e.g. etc/lists/misp-malicious-ip).
func (c *Client) RegisteredLists(ctx context.Context) ([]string, error) {
	var data itemsData
	if err := c.call(ctx, http.MethodGet, "/manager/configuration?section=ruleset&field=list", nil, &data); err != nil {
		return nil, err
	}
	var out []string
	for _, raw := range data.AffectedItems {
		var item struct {
			Ruleset struct {
				List []string `json:"list"`
			} `json:"ruleset"`
		}
		if err := json.Unmarshal(raw, &item); err != nil {
			return nil, fmt.Errorf("decoding ruleset lists: %w", err)
		}
		out = append(out, item.Ruleset.List...)
	}
	return out, nil
}

// GroupConfig returns the agent.conf of an agent group, or ErrNotFound
// when the group does not exist. The API returns it re-indented.
func (c *Client) GroupConfig(ctx context.Context, group string) ([]byte, error) {
	path := "/groups/" + url.PathEscape(group) + "/files/agent.conf?raw=true"
	code, ctype, raw, err := c.do(ctx, http.MethodGet, path, "application/json", nil)
	if err != nil {
		return nil, err
	}
	if code == http.StatusOK && !strings.Contains(ctype, "json") {
		return raw, nil
	}
	if fc, msg := firstFailure(raw); fc == codeGroupNotFound || code == http.StatusNotFound {
		return nil, ErrNotFound
	} else if msg != "" {
		return nil, &APIError{Method: http.MethodGet, Path: path, Code: code, Message: msg}
	}
	return nil, &APIError{Method: http.MethodGet, Path: path, Code: code, Message: truncate(string(raw))}
}

// CreateGroup creates an agent group.
func (c *Client) CreateGroup(ctx context.Context, group string) error {
	return c.call(ctx, http.MethodPost, "/groups", map[string]string{"group_id": group}, nil)
}

// PutGroupConfig replaces an agent group's agent.conf. The API checks
// the XML first.
func (c *Client) PutGroupConfig(ctx context.Context, group string, content []byte) error {
	return c.rawWrite(ctx, http.MethodPut, "/groups/"+url.PathEscape(group)+"/configuration", "application/xml", content)
}
