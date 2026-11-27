// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package wazuhcentral

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-cli/pkg/siem/report"
)

const (
	alertsTitle   = "*:wazuh-alerts-*"
	archivesTitle = "*:wazuh-archives-*"
	timeField     = "timestamp"
)

type savedPattern struct {
	id, title, timeField string
}

// fakeDashboard serves the OpenSearch Dashboards saved-objects find and
// create calls for index patterns, and the advanced settings.
type fakeDashboard struct {
	mu           sync.Mutex
	patterns     []savedPattern
	defaultIndex string
	failCreate   bool
	creates      int
	settingPosts int
}

func (f *fakeDashboard) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if u, p, _ := r.BasicAuth(); u != testUser || p != testPass {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if r.Header.Get("securitytenant") != "" {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	if r.Method != http.MethodGet && r.Header.Get(XSRFHeader) != XSRFValue {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"Request must contain a osd-xsrf header."}`))
		return
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/saved_objects/_find":
		q := r.URL.Query()
		if q.Get("type") != kindIndexPattern || q.Get("per_page") != "1000" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		objs := []map[string]any{}
		for _, p := range f.patterns {
			objs = append(objs, map[string]any{"id": p.id, "type": kindIndexPattern, "attributes": map[string]string{"title": p.title}})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"page": 1, "per_page": 1000, "total": len(objs), "saved_objects": objs})
	case r.Method == http.MethodPost && r.URL.Path == "/api/saved_objects/index-pattern":
		if f.failCreate {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message":"boom"}`))
			return
		}
		var body struct {
			Attributes struct {
				Title         string `json:"title"`
				TimeFieldName string `json:"timeFieldName"`
			} `json:"attributes"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.creates++
		p := savedPattern{id: fmt.Sprintf("gen-%d", f.creates), title: body.Attributes.Title, timeField: body.Attributes.TimeFieldName}
		f.patterns = append(f.patterns, p)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": p.id, "type": kindIndexPattern})
	case r.URL.Path == "/api/opensearch-dashboards/settings" && r.Method == http.MethodGet:
		settings := map[string]any{"buildNum": map[string]any{"userValue": 1}}
		if f.defaultIndex != "" {
			settings["defaultIndex"] = map[string]any{"userValue": f.defaultIndex}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"settings": settings})
	case r.URL.Path == "/api/opensearch-dashboards/settings" && r.Method == http.MethodPost:
		var body struct {
			Changes map[string]string `json:"changes"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.settingPosts++
		f.defaultIndex = body.Changes["defaultIndex"]
		_ = json.NewEncoder(w).Encode(map[string]any{"settings": map[string]any{}})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func newDashboard(t *testing.T, f *fakeDashboard) *Client {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return NewDashboardClient(srv.URL, testUser, testPass, srv.Client())
}

func wantPatterns() []IndexPattern {
	return []IndexPattern{
		{Title: alertsTitle, TimeFieldName: timeField, Default: true},
		{Title: archivesTitle, TimeFieldName: timeField},
	}
}

func resultsByKey(t *testing.T, results []report.Result) map[string]report.Result {
	t.Helper()
	out := map[string]report.Result{}
	for _, r := range results {
		assert.Equal(t, Component, r.Component)
		out[r.Kind+"/"+r.Name] = r
	}
	return out
}

func TestIndexPatternsExistAndDefaultFine(t *testing.T) {
	t.Parallel()
	f := &fakeDashboard{
		patterns:     []savedPattern{{id: "a1", title: alertsTitle, timeField: "@timestamp"}, {id: "b1", title: archivesTitle}, {id: "local", title: "wazuh-alerts-*"}},
		defaultIndex: "local",
	}
	got := resultsByKey(t, ReconcileIndexPatterns(context.Background(), newDashboard(t, f), wantPatterns(), false))
	assert.Equal(t, report.ActionOK, got["index-pattern/"+alertsTitle].Action)
	assert.Equal(t, report.ActionOK, got["index-pattern/"+archivesTitle].Action)
	assert.Equal(t, report.ActionOK, got["default-index/defaultIndex"].Action, "an existing default is left alone")
	assert.Zero(t, f.creates)
	assert.Zero(t, f.settingPosts)
	assert.Equal(t, "@timestamp", f.patterns[0].timeField, "an existing pattern is not modified")
	assert.Equal(t, "local", f.defaultIndex)
}

func TestIndexPatternsCreateAndSetDefault(t *testing.T) {
	t.Parallel()
	f := &fakeDashboard{patterns: []savedPattern{{id: "b1", title: archivesTitle}}}
	got := resultsByKey(t, ReconcileIndexPatterns(context.Background(), newDashboard(t, f), wantPatterns(), false))
	assert.Equal(t, report.ActionCreate, got["index-pattern/"+alertsTitle].Action)
	assert.Equal(t, report.ActionOK, got["index-pattern/"+archivesTitle].Action)
	assert.Equal(t, report.ActionCreate, got["default-index/defaultIndex"].Action)
	assert.Equal(t, 1, f.creates)
	require.Len(t, f.patterns, 2)
	assert.Equal(t, savedPattern{id: "gen-1", title: alertsTitle, timeField: timeField}, f.patterns[1])
	assert.Equal(t, "gen-1", f.defaultIndex)

	// Second run converges.
	again := ReconcileIndexPatterns(context.Background(), newDashboard(t, f), wantPatterns(), false)
	for _, r := range again {
		assert.Equal(t, report.ActionOK, r.Action, "%s %s", r.Kind, r.Name)
	}
}

func TestIndexPatternsDefaultPointsToMissingID(t *testing.T) {
	t.Parallel()
	f := &fakeDashboard{patterns: []savedPattern{{id: "a1", title: alertsTitle}, {id: "b1", title: archivesTitle}}, defaultIndex: "deleted"}
	got := resultsByKey(t, ReconcileIndexPatterns(context.Background(), newDashboard(t, f), wantPatterns(), false))
	assert.Equal(t, report.ActionUpdate, got["default-index/defaultIndex"].Action)
	assert.Equal(t, "a1", f.defaultIndex)
}

func TestIndexPatternsDryRun(t *testing.T) {
	t.Parallel()
	f := &fakeDashboard{defaultIndex: "deleted"}
	got := resultsByKey(t, ReconcileIndexPatterns(context.Background(), newDashboard(t, f), wantPatterns(), true))
	assert.Equal(t, report.ActionCreate, got["index-pattern/"+alertsTitle].Action)
	assert.Equal(t, report.ActionCreate, got["index-pattern/"+archivesTitle].Action)
	assert.Equal(t, report.ActionUpdate, got["default-index/defaultIndex"].Action)
	assert.Zero(t, f.creates)
	assert.Zero(t, f.settingPosts)
	assert.Equal(t, "deleted", f.defaultIndex)
}

func TestIndexPatternsAPIErrors(t *testing.T) {
	t.Parallel()
	// Unreadable: every wanted object is an error, nothing is written.
	bad := NewDashboardClient(newDashboard(t, &fakeDashboard{}).BaseURL, testUser, "wrong", http.DefaultClient)
	results := ReconcileIndexPatterns(context.Background(), bad, wantPatterns(), false)
	require.Len(t, results, 3)
	for _, r := range results {
		assert.Equal(t, report.ActionError, r.Action, "%s %s", r.Kind, r.Name)
	}
	assert.Contains(t, results[0].Detail, "HTTP 401")

	// A failed create is reported per pattern; the run goes on and the
	// default is not pointed at a pattern that does not exist.
	f := &fakeDashboard{failCreate: true, patterns: []savedPattern{{id: "b1", title: archivesTitle}}}
	got := resultsByKey(t, ReconcileIndexPatterns(context.Background(), newDashboard(t, f), wantPatterns(), false))
	assert.Equal(t, report.ActionError, got["index-pattern/"+alertsTitle].Action)
	assert.Contains(t, got["index-pattern/"+alertsTitle].Detail, "HTTP 500")
	assert.Equal(t, report.ActionOK, got["index-pattern/"+archivesTitle].Action)
	assert.Equal(t, report.ActionError, got["default-index/defaultIndex"].Action)
	assert.Zero(t, f.settingPosts)
}

func TestIndexPatternsNoDefault(t *testing.T) {
	t.Parallel()
	f := &fakeDashboard{}
	results := ReconcileIndexPatterns(context.Background(), newDashboard(t, f), []IndexPattern{{Title: archivesTitle, TimeFieldName: timeField}}, false)
	require.Len(t, results, 1)
	assert.Equal(t, report.ActionCreate, results[0].Action)
	assert.Zero(t, f.settingPosts)
}
