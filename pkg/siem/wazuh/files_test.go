// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package wazuh

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeFiles serves the ruleset file, validation, reload and group
// endpoints the way Wazuh 4.14 answers them (checked against a
// wazuh/wazuh-manager:4.14.3 container).
type fakeFiles struct {
	mu            sync.Mutex
	files         map[string]string // "rules/x.xml"
	groups        map[string]string
	clusterless   bool
	reloadMsg     string
	lastUploadCT  string
	lastRelDir    string
	registered    []string
	validationBad bool
}

func (f *fakeFiles) notFound(w http.ResponseWriter, code int) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"data":    map[string]any{"affected_items": []any{}, "failed_items": []any{map[string]any{"error": map[string]any{"code": code, "message": "not found"}}}},
		errKey:    1,
		"message": "none",
	})
}

//nolint:gocognit,gocyclo // a single fake router
func (f *fakeFiles) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.URL.Path == "/security/user/authenticate" {
		_, _ = w.Write([]byte("jwt"))
		return
	}
	p := r.URL.Path
	notFoundCode := map[string]int{"rules": 1415, "decoders": 1503, "lists": 1802}
	for kind, code := range notFoundCode {
		prefix := "/" + kind + "/files/"
		if !strings.HasPrefix(p, prefix) {
			continue
		}
		key := kind + "/" + strings.TrimPrefix(p, prefix)
		f.lastRelDir = r.URL.Query().Get("relative_dirname")
		switch r.Method {
		case http.MethodGet:
			v, ok := f.files[key]
			if !ok {
				f.notFound(w, code)
				return
			}
			w.Header().Set("Content-Type", "application/xml; charset=utf-8")
			_, _ = w.Write([]byte(v))
		case http.MethodPut:
			body, _ := io.ReadAll(r.Body)
			f.lastUploadCT = r.Header.Get("Content-Type")
			if strings.Contains(string(body), "<bogus") {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"data": map[string]any{"affected_items": []any{}, "failed_items": []any{map[string]any{"error": map[string]any{"code": 1113, "message": "XML syntax error"}}}},
					errKey: 1, "message": "Could not upload rule",
				})
				return
			}
			f.files[key] = string(body)
			affected(w, []string{"etc/" + key})
		case http.MethodDelete:
			if _, ok := f.files[key]; !ok {
				f.notFound(w, code)
				return
			}
			delete(f.files, key)
			affected(w, []string{"etc/" + key})
		}
		return
	}
	switch {
	case p == "/manager/configuration/validation":
		status := "OK"
		if f.validationBad {
			status = "KO"
		}
		affected(w, []map[string]string{{"name": "master", "status": status}})
	case p == "/cluster/analysisd/reload":
		if f.clusterless {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"title": "Bad Request", "detail": "Cluster is not running, it might be disabled", errKey: 3013})
			return
		}
		affected(w, []map[string]string{{"name": "master", "msg": f.reloadMsg}})
	case p == "/manager/analysisd/reload":
		affected(w, []map[string]string{{"name": "manager", "msg": f.reloadMsg}})
	case p == "/manager/configuration":
		affected(w, []map[string]any{{"ruleset": map[string]any{"list": f.registered}}})
	case p == "/groups" && r.Method == http.MethodPost:
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.groups[body["group_id"]] = ""
		_ = json.NewEncoder(w).Encode(map[string]any{"message": "created", errKey: 0})
	case strings.HasSuffix(p, "/files/agent.conf"):
		g := strings.TrimSuffix(strings.TrimPrefix(p, "/groups/"), "/files/agent.conf")
		v, ok := f.groups[g]
		if !ok {
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{"title": "Resource Not Found", errKey: 1710})
			return
		}
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		_, _ = w.Write([]byte(v))
	case strings.HasSuffix(p, "/configuration") && strings.HasPrefix(p, "/groups/"):
		g := strings.TrimSuffix(strings.TrimPrefix(p, "/groups/"), "/configuration")
		body, _ := io.ReadAll(r.Body)
		f.lastUploadCT = r.Header.Get("Content-Type")
		f.groups[g] = "  " + strings.ReplaceAll(string(body), "\n", "\n  ")
		_ = json.NewEncoder(w).Encode(map[string]any{"message": "updated", errKey: 0})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func newFilesClient(t *testing.T, f *fakeFiles) *Client {
	t.Helper()
	if f.files == nil {
		f.files = map[string]string{}
	}
	if f.groups == nil {
		f.groups = map[string]string{}
	}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return &Client{BaseURL: srv.URL, Username: testUser, Password: testPass, HTTP: srv.Client()}
}

func TestFileRoundTrip(t *testing.T) {
	ctx := context.Background()
	f := &fakeFiles{}
	c := newFilesClient(t, f)

	_, err := c.GetFile(ctx, "rules", "kubesoc-x.xml")
	require.ErrorIs(t, err, ErrNotFound)
	assert.Equal(t, "etc/rules", f.lastRelDir, "reads are limited to the user directory")

	require.NoError(t, c.PutFile(ctx, "rules", "kubesoc-x.xml", []byte("<group/>\n")))
	assert.Equal(t, "application/octet-stream", f.lastUploadCT)
	got, err := c.GetFile(ctx, "rules", "kubesoc-x.xml")
	require.NoError(t, err)
	assert.Equal(t, "<group/>\n", string(got))

	err = c.PutFile(ctx, "rules", "kubesoc-x.xml", []byte("<bogus/>"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "XML syntax error")

	require.NoError(t, c.DeleteFile(ctx, "rules", "kubesoc-x.xml"))
	require.NoError(t, c.DeleteFile(ctx, "rules", "kubesoc-x.xml"), "deleting a missing file is fine")

	_, err = c.GetFile(ctx, "lists", "misp-ip")
	require.ErrorIs(t, err, ErrNotFound)
	_, err = c.GetFile(ctx, "decoders", "d.xml")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestValidateAndReload(t *testing.T) {
	ctx := context.Background()
	f := &fakeFiles{reloadMsg: "Ruleset reload request sent successfully."}
	c := newFilesClient(t, f)
	require.NoError(t, c.ValidateConfiguration(ctx))
	w, err := c.ReloadAnalysisd(ctx)
	require.NoError(t, err)
	assert.Empty(t, w)

	f.validationBad = true
	require.Error(t, c.ValidateConfiguration(ctx))

	f.reloadMsg = "(7617): Signature ID '999999' was not found in rule '100901'., (7619): Empty 'if_sid' value. Rule '100901' will be ignored."
	f.clusterless = true
	w, err = c.ReloadAnalysisd(ctx)
	require.NoError(t, err, "falls back to the manager reload without a cluster")
	assert.Equal(t, []string{
		"manager: (7617): Signature ID '999999' was not found in rule '100901'.",
		"manager: (7619): Empty 'if_sid' value. Rule '100901' will be ignored.",
	}, w)
}

func TestRegisteredListsAndGroups(t *testing.T) {
	ctx := context.Background()
	f := &fakeFiles{registered: []string{"etc/lists/audit-keys", "etc/lists/misp-malicious-ip"}}
	c := newFilesClient(t, f)
	lists, err := c.RegisteredLists(ctx)
	require.NoError(t, err)
	assert.Equal(t, f.registered, lists)

	_, err = c.GroupConfig(ctx, "linux")
	require.ErrorIs(t, err, ErrNotFound)
	require.NoError(t, c.CreateGroup(ctx, "linux"))
	require.NoError(t, c.PutGroupConfig(ctx, "linux", []byte("<agent_config>\n</agent_config>\n")))
	assert.Equal(t, "application/xml", f.lastUploadCT)
	got, err := c.GroupConfig(ctx, "linux")
	require.NoError(t, err)
	assert.Contains(t, string(got), "<agent_config>")
}
