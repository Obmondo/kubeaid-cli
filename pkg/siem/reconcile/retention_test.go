// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package reconcile

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/Obmondo/kubeaid-cli/pkg/siem/config"
	"github.com/Obmondo/kubeaid-cli/pkg/siem/report"
)

const (
	userKey = "INDEXER_USERNAME"
	passKey = "INDEXER_PASSWORD"
)

// indexerStub records the ISM policies PUT to it and serves the health
// endpoints; it has no indices.
type indexerStub struct {
	mu       sync.Mutex
	policies map[string]string // id -> description
	users    []string
}

func (s *indexerStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, _, _ := r.BasicAuth()
	s.users = append(s.users, u)
	switch {
	case strings.HasPrefix(r.URL.Path, "/_plugins/_ism/policies/"):
		id := strings.TrimPrefix(r.URL.Path, "/_plugins/_ism/policies/")
		if r.Method == http.MethodGet {
			d, ok := s.policies[id]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"_seq_no": 1, "_primary_term": 1, "policy": map[string]any{"description": d}})
			return
		}
		var in struct {
			Policy struct {
				Description string `json:"description"`
			} `json:"policy"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		s.policies[id] = in.Policy.Description
		_ = json.NewEncoder(w).Encode(map[string]any{"_seq_no": 1, "_primary_term": 1})
	case strings.HasPrefix(r.URL.Path, "/_plugins/_ism/explain/"):
		_, _ = w.Write([]byte(`{"total_managed_indices":0}`))
	case r.URL.Path == "/_cluster/health":
		_, _ = w.Write([]byte(`{"status":"green","number_of_nodes":1}`))
	case r.URL.Path == "/_cat/allocation":
		_, _ = w.Write([]byte(`[{"node":"n","disk.used":"50","disk.total":"200"}]`))
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func TestRunRetention(t *testing.T) {
	t.Parallel()
	stub := &indexerStub{policies: map[string]string{}}
	srv := httptest.NewTLSServer(stub)
	t.Cleanup(srv.Close)
	cfg := loadExample(t)
	for i := range cfg.Components.Wazuh {
		cfg.Components.Wazuh[i].Indexer.URL = srv.URL
	}
	centralStub := &indexerStub{policies: map[string]string{}}
	central := httptest.NewTLSServer(centralStub)
	t.Cleanup(central.Close)
	cfg.Components.WazuhCentral.URL = central.URL
	kube := fake.NewClientset(
		secret("wazuh-001", "wazuh-indexer-cred", map[string]string{userKey: "admin-001", passKey: "p"}),
		secret("security-operations", "wazuh-indexer-cred", map[string]string{userKey: "admin-central", passKey: "p"}),
	)

	results := Run(context.Background(), Options{Config: cfg, Kube: kube, Only: []string{ComponentRetention}})
	got := map[string]report.Action{}
	for _, r := range results {
		got[r.Component+" "+r.Kind+" "+r.Name] = r.Action
	}
	assert.Equal(t, report.ActionCreate, got["retention/001 ism-policy kubesoc-retention"])
	assert.Equal(t, report.ActionOK, got["retention/001 ism-indices wazuh-alerts-*"])
	assert.Equal(t, report.ActionError, got["retention/002 setup indexer"], "no credentials for 002")
	assert.Equal(t, report.ActionCreate, got["retention/central ism-policy kubesoc-retention"])
	assert.Equal(t, report.ActionOK, got["retention/central ism-indices wazuh-monitoring-*"])
	assert.NotContains(t, got, "retention/central ism-indices wazuh-alerts-*", "central holds no events")
	assert.Equal(t, []string{"admin-001"}, uniq(stub.users))
	assert.Equal(t, []string{"admin-central"}, uniq(centralStub.users))
	assert.Contains(t, stub.policies["kubesoc-retention"], "delete after 90d", "tenant 001")
	assert.Contains(t, centralStub.policies["kubesoc-retention"], "delete after 90d", "centralDays")
}

func uniq(in []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func TestRetentionPolicyFromConfig(t *testing.T) {
	t.Parallel()
	cfg := loadExample(t)
	p := RetentionPolicy(cfg, cfg.Tenants[1].RetentionDays, false)
	assert.Equal(t, 180, p.RetentionDays)
	assert.Equal(t, 30, p.WarmAfterDays)
	assert.Equal(t, config.DefaultRetentionIndexPatterns, p.IndexPatterns)
	assert.Equal(t, config.DefaultCentralRetentionIndexPatterns, RetentionPolicy(cfg, 90, true).IndexPatterns)
}

func TestRunWithoutRetentionSkipsIt(t *testing.T) {
	t.Parallel()
	cfg := loadExample(t)
	cfg.Retention = nil
	results := Run(context.Background(), Options{Config: cfg, Kube: fake.NewClientset(), DryRun: true, Only: []string{ComponentRetention}})
	assert.Empty(t, results)
}

func TestProbe(t *testing.T) {
	t.Parallel()
	stub := &indexerStub{policies: map[string]string{}}
	idx := httptest.NewTLSServer(stub)
	t.Cleanup(idx.Close)
	mgr := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/security/user/authenticate":
			_, _ = w.Write([]byte("jwt"))
		case "/agents/summary/status":
			_, _ = w.Write([]byte(`{"error":0,"data":{"connection":{"active":3,"disconnected":1,"total":4}}}`))
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	t.Cleanup(mgr.Close)
	kc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/realms/soc/.well-known/openid-configuration" {
			_, _ = w.Write([]byte(`{}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(kc.Close)

	cfg := loadExample(t)
	cfg.Keycloak.URL = kc.URL + "/auth"
	for i := range cfg.Components.Wazuh {
		cfg.Components.Wazuh[i].URL = mgr.URL
		cfg.Components.Wazuh[i].Indexer.URL = idx.URL
	}
	cfg.Components.WazuhCentral.URL = "https://127.0.0.1:1"
	cfg.Components.Velociraptor.Address = strings.TrimPrefix(kc.URL, "http://")
	kube := fake.NewClientset(
		secret("wazuh-001", "wazuh-indexer-cred", map[string]string{userKey: "a", passKey: "p"}),
		secret("wazuh-001", "wazuh-api-cred", map[string]string{"API_USERNAME": "a", "API_PASSWORD": "p"}),
		secret("security-operations", "wazuh-indexer-cred", map[string]string{userKey: "a", passKey: "p"}),
	)
	snap := Probe(context.Background(), Options{Config: cfg, Kube: kube})

	require.Len(t, snap.Indexers, 3)
	assert.True(t, snap.Indexers[0].Up)
	assert.Equal(t, "green", snap.Indexers[0].Status)
	assert.InDelta(t, 25.0, snap.Indexers[0].DiskUsedPercent, 0.001)
	assert.False(t, snap.Indexers[1].Up, "no credentials for 002")
	assert.Equal(t, CentralScope, snap.Indexers[2].Name)
	assert.False(t, snap.Indexers[2].Up)

	require.Len(t, snap.Managers, 2)
	assert.True(t, snap.Managers[0].Up)
	assert.Equal(t, 1, snap.Managers[0].AgentsDisconnected)
	assert.False(t, snap.Managers[0].StatsAvailable)
	assert.False(t, snap.Managers[1].Up)

	up := map[string]bool{}
	for _, c := range snap.Components {
		up[c.Name] = c.Up
	}
	assert.Equal(t, map[string]bool{HealthKeycloak: true, HealthIRIS: false, HealthVelociraptor: true}, up)
	assert.False(t, snap.Healthy())
	for _, a := range kube.Actions() {
		assert.Equal(t, "get", a.GetVerb(), "the probe only reads")
	}
}
