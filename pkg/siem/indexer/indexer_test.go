// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package indexer

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-cli/pkg/siem/report"
)

const (
	testUser   = "admin"
	testPass   = "pw"
	policyID   = "kubesoc-retention"
	alertsPat  = "wazuh-alerts-*"
	archPat    = "wazuh-archives-*"
	alertsDay1 = "wazuh-alerts-4.x-2026.09.01"
	alertsDay2 = "wazuh-alerts-4.x-2026.09.02"
	otherIndex = "wazuh-alerts-4.x-2026.08.31"

	keyError     = "error"
	keyPolicyDoc = "policy"
	keyDiskTotal = "disk.total"
	keyDiskUsed  = "disk.used"
	keyNode      = "node"
)

type fakePolicy struct {
	seqNo       int64
	primaryTerm int64
	body        map[string]any
}

type fakeIndex struct {
	policy *string
	seqNo  *int64
}

// fakeISM serves the ISM policy, explain, add and change_policy APIs
// and the health endpoints of an OpenSearch cluster.
type fakeISM struct {
	mu       sync.Mutex
	policies map[string]*fakePolicy
	indices  map[string]*fakeIndex
	writes   int
	seq      int64
}

func newFakeISM() *fakeISM {
	return &fakeISM{policies: map[string]*fakePolicy{}, indices: map[string]*fakeIndex{}}
}

func ptr[T any](v T) *T { return &v }

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (f *fakeISM) match(patterns string) []string {
	var out []string
	for name := range f.indices {
		for _, p := range strings.Split(patterns, ",") {
			if ok, _ := path.Match(p, name); ok || p == name {
				out = append(out, name)
				break
			}
		}
	}
	return out
}

//nolint:gocognit,gocyclo // a single fake router
func (f *fakeISM) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if u, p, ok := r.BasicAuth(); !ok || u != testUser || p != testPass {
		writeJSON(w, http.StatusUnauthorized, map[string]any{})
		return
	}
	p := r.URL.Path
	switch {
	case strings.HasPrefix(p, "/_plugins/_ism/policies/"):
		id := strings.TrimPrefix(p, "/_plugins/_ism/policies/")
		cur := f.policies[id]
		if r.Method == http.MethodGet {
			if cur == nil {
				writeJSON(w, http.StatusNotFound, map[string]any{keyError: "not found"})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"_id": id, "_seq_no": cur.seqNo, "_primary_term": cur.primaryTerm, keyPolicyDoc: cur.body})
			return
		}
		var in struct {
			Policy map[string]any `json:"policy"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		q := r.URL.Query()
		if cur != nil && q.Get("if_seq_no") != strconv.FormatInt(cur.seqNo, 10) {
			writeJSON(w, http.StatusConflict, map[string]any{keyError: "version conflict"})
			return
		}
		f.writes++
		f.seq++
		// OpenSearch adds fields to the stored policy.
		in.Policy["schema_version"] = 21
		f.policies[id] = &fakePolicy{seqNo: f.seq, primaryTerm: 1, body: in.Policy}
		code := http.StatusOK
		if cur == nil {
			code = http.StatusCreated
		}
		writeJSON(w, code, map[string]any{"_id": id, "_seq_no": f.seq, "_primary_term": 1})
	case strings.HasPrefix(p, "/_plugins/_ism/explain/"):
		out := map[string]any{}
		managed := 0
		for _, name := range f.match(strings.TrimPrefix(p, "/_plugins/_ism/explain/")) {
			ix := f.indices[name]
			e := map[string]any{"index.plugins.index_state_management.policy_id": ix.policy, "index.opendistro.index_state_management.policy_id": ix.policy}
			if ix.policy != nil {
				managed++
				if ix.seqNo != nil {
					e["policy_seq_no"] = *ix.seqNo
				}
			}
			out[name] = e
		}
		out["total_managed_indices"] = managed
		writeJSON(w, http.StatusOK, out)
	case strings.HasPrefix(p, "/_plugins/_ism/add/"):
		var in struct {
			PolicyID string `json:"policy_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		if f.policies[in.PolicyID] == nil {
			writeJSON(w, http.StatusNotFound, map[string]any{keyError: "no policy"})
			return
		}
		f.writes++
		n := 0
		for _, name := range f.match(strings.TrimPrefix(p, "/_plugins/_ism/add/")) {
			if f.indices[name].policy == nil {
				f.indices[name].policy = ptr(in.PolicyID)
				f.indices[name].seqNo = ptr(f.policies[in.PolicyID].seqNo)
				n++
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"updated_indices": n, "failures": false, "failed_indices": []any{}})
	case strings.HasPrefix(p, "/_plugins/_ism/change_policy/"):
		var in struct {
			PolicyID string `json:"policy_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		f.writes++
		n := 0
		for _, name := range strings.Split(strings.TrimPrefix(p, "/_plugins/_ism/change_policy/"), ",") {
			if ix := f.indices[name]; ix != nil {
				ix.policy = ptr(in.PolicyID)
				ix.seqNo = ptr(f.policies[in.PolicyID].seqNo)
				n++
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"updated_indices": n, "failures": false, "failed_indices": []any{}})
	case p == "/_cluster/health":
		writeJSON(w, http.StatusOK, map[string]any{"status": "yellow", "number_of_nodes": 2})
	case p == "/_cat/allocation":
		writeJSON(w, http.StatusOK, []map[string]any{
			{keyNode: "a", keyDiskUsed: "80", keyDiskTotal: "100", "disk.percent": "80"},
			{keyNode: "b", keyDiskUsed: "10", keyDiskTotal: "100", "disk.percent": "10"},
			{keyNode: "UNASSIGNED", keyDiskUsed: nil, keyDiskTotal: nil},
		})
	default:
		writeJSON(w, http.StatusNotFound, map[string]any{keyError: "no route " + p})
	}
}

func newClient(t *testing.T, f *fakeISM) *Client {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return &Client{BaseURL: srv.URL, Username: testUser, Password: testPass, HTTP: srv.Client()}
}

func testPolicy(days int) Policy {
	return Policy{ID: policyID, RetentionDays: days, IndexPatterns: []string{alertsPat, archPat}}
}

func actions(results []report.Result) map[string]report.Action {
	out := map[string]report.Action{}
	for _, r := range results {
		out[r.Kind+" "+r.Name] = r.Action
	}
	return out
}

func TestRetentionCreatesPolicyAndAttaches(t *testing.T) {
	f := newFakeISM()
	f.indices[alertsDay1] = &fakeIndex{}
	f.indices[alertsDay2] = &fakeIndex{}
	f.indices[otherIndex] = &fakeIndex{policy: ptr("someone-elses")}
	c := newClient(t, f)
	comp := ComponentName("001")

	// Dry run writes nothing.
	res := Reconcile(context.Background(), c, comp, testPolicy(90), true)
	assert.Equal(t, 0, f.writes)
	assert.Equal(t, report.ActionCreate, actions(res)[KindPolicy+" "+policyID])
	assert.Equal(t, report.ActionCreate, actions(res)[KindIndices+" "+alertsPat])
	assert.Equal(t, report.ActionOK, actions(res)[KindIndices+" "+archPat])

	res = Reconcile(context.Background(), c, comp, testPolicy(90), false)
	assert.Zero(t, report.Summarize(res).Errors, res)
	require.NotNil(t, f.policies[policyID])
	assert.Equal(t, policyID, *f.indices[alertsDay1].policy)
	assert.Equal(t, "someone-elses", *f.indices[otherIndex].policy, "foreign policy left alone")
	body := f.policies[policyID].body
	assert.Contains(t, body["description"], "delete after 90d")
	templates, ok := body["ism_template"].([]any)
	require.True(t, ok)
	tmpl, ok := templates[0].(map[string]any)
	require.True(t, ok)
	assert.ElementsMatch(t, []any{alertsPat, archPat}, tmpl["index_patterns"])
	for _, r := range res {
		assert.Equal(t, comp, r.Component)
	}

	// Second run: nothing to do.
	writes := f.writes
	res = Reconcile(context.Background(), c, comp, testPolicy(90), false)
	assert.Equal(t, writes, f.writes)
	assert.Zero(t, report.Summarize(res).Changes, res)
	for _, r := range res {
		if r.Name == alertsPat {
			assert.Equal(t, "2 indices managed, 1 under another policy left alone", r.Detail)
		}
	}
}

func TestRetentionUpdateMovesManagedIndices(t *testing.T) {
	f := newFakeISM()
	f.indices[alertsDay1] = &fakeIndex{}
	c := newClient(t, f)
	comp := ComponentName("001")
	Reconcile(context.Background(), c, comp, testPolicy(90), false)
	old := *f.indices[alertsDay1].seqNo

	res := Reconcile(context.Background(), c, comp, testPolicy(30), true)
	assert.Equal(t, report.ActionUpdate, actions(res)[KindPolicy+" "+policyID])
	assert.Equal(t, report.ActionUpdate, actions(res)[KindIndices+" "+alertsPat])
	assert.Equal(t, old, *f.indices[alertsDay1].seqNo, "dry run")

	res = Reconcile(context.Background(), c, comp, testPolicy(30), false)
	assert.Zero(t, report.Summarize(res).Errors, res)
	assert.Contains(t, f.policies[policyID].body["description"], "delete after 30d")
	assert.Equal(t, f.policies[policyID].seqNo, *f.indices[alertsDay1].seqNo)
	assert.Equal(t, report.ActionUpdate, actions(res)[KindIndices+" "+alertsPat])
}

// policyStates is the state list of a rendered policy.
func policyStates(t *testing.T, p Policy) []any {
	t.Helper()
	states, ok := p.Body()["states"].([]any)
	require.True(t, ok)
	return states
}

func TestRetentionWarmState(t *testing.T) {
	p := testPolicy(365)
	p.WarmAfterDays = 30
	states := policyStates(t, p)
	require.Len(t, states, 3)
	warm, ok := states[1].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, stateWarm, warm["name"])
	assert.NotEqual(t, testPolicy(365).Fingerprint(), p.Fingerprint())

	// A warm age past the retention is ignored.
	p.WarmAfterDays = 400
	assert.Len(t, policyStates(t, p), 2)
	assert.Equal(t, testPolicy(365).Fingerprint(), p.Fingerprint())
}

func TestRetentionSkipsWithoutDays(t *testing.T) {
	f := newFakeISM()
	res := Reconcile(context.Background(), newClient(t, f), ComponentName("001"), testPolicy(0), false)
	require.Len(t, res, 1)
	assert.Equal(t, report.ActionSkip, res[0].Action)
	assert.Equal(t, 0, f.writes)
}

func TestRetentionReportsUnreachableIndexer(t *testing.T) {
	c := &Client{BaseURL: "http://127.0.0.1:1", Username: testUser, Password: testPass, HTTP: http.DefaultClient}
	res := Reconcile(context.Background(), c, ComponentName("001"), testPolicy(30), false)
	require.Len(t, res, 1)
	assert.Equal(t, report.ActionError, res[0].Action)
}

func TestHealth(t *testing.T) {
	h, err := newClient(t, newFakeISM()).Health(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "yellow", h.Status)
	assert.Equal(t, 2, h.Nodes)
	assert.InDelta(t, 80.0, h.DiskUsedPercent, 0.001)
	assert.InDelta(t, 200.0, h.DiskTotalBytes, 0.001)
}
