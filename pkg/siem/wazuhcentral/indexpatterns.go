// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package wazuhcentral

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Obmondo/kubeaid-cli/pkg/siem/report"
)

const (
	kindIndexPattern = "index-pattern"
	kindDefaultIndex = "default-index"
	nameDefaultIndex = "defaultIndex"

	findIndexPatternsPath = "/api/saved_objects/_find?type=index-pattern&fields=title&per_page=1000"
	createIndexPattern    = "/api/saved_objects/index-pattern"
	settingsPath          = "/api/opensearch-dashboards/settings"

	// XSRFHeader must be set (to XSRFValue) on dashboard writes.
	XSRFHeader = "osd-xsrf"
	XSRFValue  = "true"
)

// IndexPattern is one saved index pattern on the central dashboard.
type IndexPattern struct {
	Title         string
	TimeFieldName string
	// Default makes it the dashboard's defaultIndex when none is set or
	// the current one no longer exists.
	Default bool
}

// NewDashboardClient returns a client for the OpenSearch Dashboards
// saved-objects and settings APIs. It authenticates with basic auth and
// sends osd-xsrf on every request; it sends no securitytenant header
// (the global tenant is the default, and asking for it by name is
// refused).
func NewDashboardClient(baseURL, username, password string, hc *http.Client) *Client {
	return &Client{
		BaseURL:  baseURL,
		Username: username,
		Password: password,
		HTTP:     hc,
		headers:  map[string]string{XSRFHeader: XSRFValue},
	}
}

// indexPatterns returns title -> id of every saved index pattern (the
// first id wins for a duplicated title), and the set of all ids.
func (c *Client) indexPatterns(ctx context.Context) (map[string]string, map[string]bool, error) {
	var out struct {
		SavedObjects []struct {
			ID         string `json:"id"`
			Attributes struct {
				Title string `json:"title"`
			} `json:"attributes"`
		} `json:"saved_objects"`
	}
	if err := c.call(ctx, http.MethodGet, findIndexPatternsPath, nil, &out); err != nil {
		return nil, nil, err
	}
	byTitle := map[string]string{}
	ids := map[string]bool{}
	for _, o := range out.SavedObjects {
		ids[o.ID] = true
		if _, ok := byTitle[o.Attributes.Title]; !ok {
			byTitle[o.Attributes.Title] = o.ID
		}
	}
	return byTitle, ids, nil
}

// createIndexPattern creates a pattern and returns the id the server
// chose.
func (c *Client) createIndexPattern(ctx context.Context, p IndexPattern) (string, error) {
	in := map[string]any{"attributes": map[string]string{"title": p.Title, "timeFieldName": p.TimeFieldName}}
	var out struct {
		ID string `json:"id"`
	}
	if err := c.call(ctx, http.MethodPost, createIndexPattern, in, &out); err != nil {
		return "", err
	}
	if out.ID == "" {
		return "", errors.New("dashboard returned no id for the new index pattern")
	}
	return out.ID, nil
}

// defaultIndex returns the dashboard's defaultIndex setting ("" when
// unset).
func (c *Client) defaultIndex(ctx context.Context) (string, error) {
	var out struct {
		Settings map[string]struct {
			UserValue json.RawMessage `json:"userValue"`
		} `json:"settings"`
	}
	if err := c.call(ctx, http.MethodGet, settingsPath, nil, &out); err != nil {
		return "", err
	}
	var id string
	if raw := out.Settings[nameDefaultIndex].UserValue; len(raw) > 0 {
		_ = json.Unmarshal(raw, &id) // null or a non-string counts as unset
	}
	return id, nil
}

func (c *Client) setDefaultIndex(ctx context.Context, id string) error {
	return c.call(ctx, http.MethodPost, settingsPath, map[string]any{"changes": map[string]string{nameDefaultIndex: id}}, nil)
}

// ReconcileIndexPatterns ensures a saved index pattern exists for every
// title in want and, for the one marked Default, that the dashboard's
// defaultIndex points to an existing pattern. Existing patterns are
// matched by exact title and never modified; a defaultIndex that
// already points to an existing pattern is left alone. Patterns not in
// want are ignored.
func ReconcileIndexPatterns(ctx context.Context, c *Client, want []IndexPattern, dryRun bool) []report.Result {
	var results []report.Result
	add := func(kind, name string, a report.Action, detail string) {
		results = append(results, report.Result{Component: Component, Kind: kind, Name: name, Action: a, Detail: detail})
	}
	if len(want) == 0 {
		return nil
	}
	var def *IndexPattern
	for i := range want {
		if want[i].Default {
			def = &want[i]
		}
	}

	byTitle, ids, err := c.indexPatterns(ctx)
	if err != nil {
		for _, p := range want {
			add(kindIndexPattern, p.Title, report.ActionError, err.Error())
		}
		if def != nil {
			add(kindDefaultIndex, nameDefaultIndex, report.ActionError, "index patterns unreadable")
		}
		return results
	}

	failed := map[string]bool{}
	for _, p := range want {
		if id, ok := byTitle[p.Title]; ok {
			add(kindIndexPattern, p.Title, report.ActionOK, "id "+id)
			continue
		}
		detail := "timeFieldName " + p.TimeFieldName
		if !dryRun {
			id, err := c.createIndexPattern(ctx, p)
			if err != nil {
				failed[p.Title] = true
				add(kindIndexPattern, p.Title, report.ActionError, err.Error())
				continue
			}
			byTitle[p.Title] = id
			ids[id] = true
			detail += ", id " + id
		}
		add(kindIndexPattern, p.Title, report.ActionCreate, detail)
	}

	if def == nil {
		return results
	}
	cur, err := c.defaultIndex(ctx)
	if err != nil {
		add(kindDefaultIndex, nameDefaultIndex, report.ActionError, err.Error())
		return results
	}
	if cur != "" && ids[cur] {
		add(kindDefaultIndex, nameDefaultIndex, report.ActionOK, "id "+cur)
		return results
	}
	action := report.ActionCreate
	if cur != "" {
		action = report.ActionUpdate
	}
	if failed[def.Title] {
		add(kindDefaultIndex, nameDefaultIndex, report.ActionError, "index pattern "+def.Title+" could not be created")
		return results
	}
	id := byTitle[def.Title] // empty only in a dry run that would create it
	detail := "set to " + def.Title
	if id != "" {
		detail += " (id " + id + ")"
	}
	if cur != "" {
		detail += ", was missing id " + cur
	}
	if !dryRun {
		if err := c.setDefaultIndex(ctx, id); err != nil {
			add(kindDefaultIndex, nameDefaultIndex, report.ActionError, err.Error())
			return results
		}
	}
	add(kindDefaultIndex, nameDefaultIndex, action, detail)
	return results
}
