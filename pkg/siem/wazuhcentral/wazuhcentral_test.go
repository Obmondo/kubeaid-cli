// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package wazuhcentral

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/Obmondo/kubeaid-cli/pkg/siem/config"
	"github.com/Obmondo/kubeaid-cli/pkg/siem/report"
)

const (
	testUser = "admin"
	testPass = "pw"
	seeds001 = "wazuh-indexer-nodes.wazuh-001.svc:9300"
	seeds002 = "wazuh-indexer-nodes.wazuh-002.svc:9300"
	alias001 = "t001"
	ns001    = "wazuh-001"
)

// fakeIndexer serves GET/PUT /_cluster/settings with flat persistent
// settings.
type fakeIndexer struct {
	mu         sync.Mutex
	persistent map[string]any
	puts       int
}

func (f *fakeIndexer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if u, p, _ := r.BasicAuth(); u != testUser || p != testPass {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"Unauthorized"}`))
		return
	}
	switch {
	case r.URL.Path != "/_cluster/settings":
		w.WriteHeader(http.StatusNotFound)
	case r.Method == http.MethodGet:
		if r.URL.Query().Get("flat_settings") != "true" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{keyPersistent: f.persistent, "transient": map[string]any{}})
	case r.Method == http.MethodPut:
		var body struct {
			Persistent map[string]any `json:"persistent"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		for k, v := range body.Persistent {
			f.persistent[k] = v
		}
		f.puts++
		_ = json.NewEncoder(w).Encode(map[string]any{"acknowledged": true, keyPersistent: body.Persistent})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func newClient(t *testing.T, f *fakeIndexer) *Client {
	t.Helper()
	srv := httptest.NewTLSServer(f)
	t.Cleanup(srv.Close)
	return &Client{BaseURL: srv.URL, Username: testUser, Password: testPass, HTTP: srv.Client()}
}

func byName(results []report.Result) map[string]report.Result {
	out := map[string]report.Result{}
	for _, r := range results {
		out[r.Kind+"/"+r.Name] = r
	}
	return out
}

func TestReconcileRemotes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := &fakeIndexer{persistent: map[string]any{
		"cluster.remote.t002.seeds":            []string{"old:9300"},
		"cluster.remote.legacy.seeds":          []string{"legacy:9300"},
		"cluster.remote.proxied.mode":          "proxy",
		"cluster.remote.proxied.proxy_address": "proxy:9400",
		"cluster.remote.connect":               "true",
	}}
	c := newClient(t, f)
	spec := Spec{Remotes: []Remote{{Alias: alias001, Seeds: []string{seeds001}}, {Alias: "t002", Seeds: []string{seeds002}}}}

	got := byName(Reconcile(ctx, c, spec, true))
	assert.Zero(t, f.puts, "dry run must not write")
	assert.Equal(t, report.ActionCreate, got["remote/t001"].Action)
	assert.Equal(t, report.ActionUpdate, got["remote/t002"].Action)
	assert.Equal(t, report.ActionSkip, got["remote/legacy"].Action)
	assert.Equal(t, report.ActionSkip, got["remote/proxied"].Action)
	assert.Contains(t, got["remote/legacy"].Detail, "extra")
	assert.Len(t, got, 4, "global cluster.remote settings are not remotes")

	results := Reconcile(ctx, c, spec, false)
	assert.Equal(t, 2, report.Summarize(results).Changes)
	assert.Equal(t, 2, f.puts)
	assert.Contains(t, f.persistent, "cluster.remote.legacy.seeds", "extra remotes are never removed")

	results = Reconcile(ctx, c, spec, false)
	assert.Zero(t, report.Summarize(results).Changes)
	assert.Equal(t, 2, f.puts, "second run must not write")
}

func TestReconcileSeedsAsString(t *testing.T) {
	t.Parallel()
	f := &fakeIndexer{persistent: map[string]any{"cluster.remote.t001.seeds": "b:9300,a:9300"}}
	spec := Spec{Remotes: []Remote{{Alias: alias001, Seeds: []string{"a:9300", "b:9300"}}}}
	got := byName(Reconcile(context.Background(), newClient(t, f), spec, false))
	assert.Equal(t, report.ActionOK, got["remote/t001"].Action, "order does not matter")
}

func TestReconcileBadCredentials(t *testing.T) {
	t.Parallel()
	c := newClient(t, &fakeIndexer{persistent: map[string]any{}})
	c.Password = "wrong"
	results := Reconcile(context.Background(), c, Spec{Remotes: []Remote{{Alias: alias001, Seeds: []string{seeds001}}}}, true)
	require.Len(t, results, 1)
	assert.Equal(t, report.ActionError, results[0].Action)
	assert.Contains(t, results[0].Detail, "401")
}

func credSecret(ns, user, pass string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "wazuh-api-cred"},
		Data:       map[string][]byte{"API_USERNAME": []byte(user), "API_PASSWORD": []byte(pass)},
	}
}

func managers() []config.Wazuh {
	ref := func(ns string) config.WazuhCredsRef {
		return config.WazuhCredsRef{Namespace: ns, Name: "wazuh-api-cred", UsernameKey: "API_USERNAME", PasswordKey: "API_PASSWORD"}
	}
	return []config.Wazuh{
		{Tenant: "001", URL: "https://wazuh.wazuh-001.svc:55000", CredSecretRef: ref(ns001)},
		{Tenant: "002", URL: "https://wazuh.wazuh-002.svc:55001", CredSecretRef: ref("wazuh-002")},
	}
}

func TestRenderDashboardConfig(t *testing.T) {
	t.Parallel()
	got := string(RenderDashboardConfig([]DashboardHost{
		{ID: FirstHostID, URL: "https://wazuh.wazuh-001.svc", Port: 55000, Username: "wazuh-wui", Password: `p"w:#`},
	}))
	assert.Equal(t, `pattern: "*:wazuh-alerts-*"
checks.pattern: false
checks.template: false
checks.fields: false
checks.api: true
checks.setup: true
hosts:
  - 1513629884013:
      url: "https://wazuh.wazuh-001.svc"
      port: 55000
      username: "wazuh-wui"
      password: "p\"w:#"
      run_as: true
`, got)
}

func TestEnsureDashboardConfig(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	target := config.ObjectRef{Namespace: "security-operations", Name: "wazuh-app-config"}
	kube := fake.NewClientset(credSecret(ns001, "wui-1", "pw-1"))

	results := EnsureDashboardConfig(ctx, kube, target, managers(), false)
	got := byName(results)
	assert.Equal(t, report.ActionError, got["dashboard-host/002"].Action)
	assert.Equal(t, report.ActionSkip, got["dashboard-config/security-operations/wazuh-app-config"].Action,
		"a partial host list is never written")
	_, err := kube.CoreV1().Secrets(target.Namespace).Get(ctx, target.Name, metav1.GetOptions{})
	require.Error(t, err)

	_, err = kube.CoreV1().Secrets("wazuh-002").Create(ctx, credSecret("wazuh-002", "wui-2", "pw-2"), metav1.CreateOptions{})
	require.NoError(t, err)
	got = byName(EnsureDashboardConfig(ctx, kube, target, managers(), true))
	assert.Equal(t, report.ActionCreate, got["dashboard-config/security-operations/wazuh-app-config"].Action)
	_, err = kube.CoreV1().Secrets(target.Namespace).Get(ctx, target.Name, metav1.GetOptions{})
	require.Error(t, err, "dry run must not write")

	got = byName(EnsureDashboardConfig(ctx, kube, target, managers(), false))
	assert.Equal(t, report.ActionCreate, got["dashboard-config/security-operations/wazuh-app-config"].Action)
	sec, err := kube.CoreV1().Secrets(target.Namespace).Get(ctx, target.Name, metav1.GetOptions{})
	require.NoError(t, err)
	yml := string(sec.Data[DashboardConfigKey])
	assert.Contains(t, yml, "  - 1513629884013:\n      url: \"https://wazuh.wazuh-001.svc\"\n      port: 55000\n")
	assert.Contains(t, yml, "  - t002:\n      url: \"https://wazuh.wazuh-002.svc\"\n      port: 55001\n")
	assert.Contains(t, yml, `password: "pw-2"`)

	got = byName(EnsureDashboardConfig(ctx, kube, target, managers(), false))
	assert.Equal(t, report.ActionOK, got["dashboard-config/security-operations/wazuh-app-config"].Action)

	cred, err := kube.CoreV1().Secrets(ns001).Get(ctx, "wazuh-api-cred", metav1.GetOptions{})
	require.NoError(t, err)
	cred.Data["API_PASSWORD"] = []byte("rotated")
	_, err = kube.CoreV1().Secrets(ns001).Update(ctx, cred, metav1.UpdateOptions{})
	require.NoError(t, err)
	res := EnsureDashboardConfig(ctx, kube, target, managers(), false)
	got = byName(res)
	assert.Equal(t, report.ActionUpdate, got["dashboard-config/security-operations/wazuh-app-config"].Action)
	for _, r := range res {
		assert.NotContains(t, r.Detail, "rotated", "values are never reported")
	}
}

func TestSplitAPIURL(t *testing.T) {
	t.Parallel()
	u, p, err := splitAPIURL("https://wazuh.wazuh-001.svc")
	require.NoError(t, err)
	assert.Equal(t, "https://wazuh.wazuh-001.svc", u)
	assert.Equal(t, 55000, p)
	u, p, err = splitAPIURL("https://[fd00::1]:55443/")
	require.NoError(t, err)
	assert.Equal(t, "https://[fd00::1]", u)
	assert.Equal(t, 55443, p)
	_, _, err = splitAPIURL("wazuh:55000")
	require.Error(t, err)
}
