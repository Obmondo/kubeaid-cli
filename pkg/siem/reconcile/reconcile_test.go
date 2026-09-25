// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package reconcile

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/Obmondo/kubeaid-cli/pkg/siem/config"
	"github.com/Obmondo/kubeaid-cli/pkg/siem/report"
	"github.com/Obmondo/kubeaid-cli/pkg/siem/wazuhcentral"
)

const exampleConfig = "../config/testdata/tenants.example.json"

func loadExample(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := config.Load(exampleConfig)
	require.NoError(t, err)
	return cfg
}

func TestParseOnly(t *testing.T) {
	t.Parallel()
	got, err := ParseOnly("keycloak, iris")
	require.NoError(t, err)
	assert.Equal(t, []string{"keycloak", "iris"}, got)
	got, err = ParseOnly("wazuh,wazuhcentral,enrolment")
	require.NoError(t, err)
	assert.Equal(t, []string{"wazuh", "wazuhcentral", "enrolment"}, got)
	got, err = ParseOnly("")
	require.NoError(t, err)
	assert.Nil(t, got)
	_, err = ParseOnly("keycloak,nope")
	require.Error(t, err)
}

func TestSpecsFromExample(t *testing.T) {
	t.Parallel()
	cfg := loadExample(t)

	is := IRISSpec(cfg)
	assert.Equal(t, "IrisInitialClient", is.InitialCustomer)
	require.Len(t, is.Customers, 2)
	assert.Equal(t, "Tenant A", is.Customers[0].Name)
	assert.Equal(t, "svc_ai", is.ServiceAccounts[0].Login)

	ws := WazuhSpec(cfg, cfg.Components.Wazuh[1])
	assert.Equal(t, "002", ws.Tenant)
	require.Len(t, ws.Mappings, 3)
	assert.Equal(t, "oidc_administrator", ws.Mappings[0].Rule)
	assert.Equal(t, "administrator", ws.Mappings[0].APIRole)
	assert.Equal(t, "readonly", ws.Mappings[1].APIRole)
	assert.Equal(t, "oidc_tenant_002", ws.Mappings[2].Rule)
	assert.Equal(t, "tenant-002", ws.Mappings[2].BackendRole)
	assert.Equal(t, "readonly", ws.Mappings[2].APIRole)

	cs := WazuhCentralSpec(cfg)
	require.Len(t, cs.Remotes, 2)
	assert.Equal(t, "t001", cs.Remotes[0].Alias)
	ips := IndexPatterns(cfg)
	require.Len(t, ips, 2)
	assert.Equal(t, wazuhcentral.IndexPattern{Title: "*:wazuh-alerts-*", TimeFieldName: "timestamp", Default: true}, ips[0])

	vs := VelociraptorSpec(cfg)
	assert.Equal(t, []string{"Tenant A", "Tenant B"}, vs.Orgs)
	assert.Equal(t, "N", vs.Monitoring[1].Parameters["DryRun"])

	var sync config.Client
	for _, c := range cfg.Clients {
		if c.ClientID == "iris-sync" {
			sync = c
		}
	}
	kc := ClientSpec(sync, "x")
	assert.Equal(t, "x", kc.Secret)
	assert.Equal(t, []string{"query-groups", "query-users", "view-users"}, sortedCopy(kc.ScopeMappingsClient["realm-management"]))
	require.NotNil(t, kc.FullScopeAllowed)
	assert.False(t, *kc.FullScopeAllowed)
}

func TestRunReportsMissingCredentialsPerComponent(t *testing.T) {
	t.Parallel()
	cfg := loadExample(t)
	kube := fake.NewClientset()

	results := Run(context.Background(), Options{Config: cfg, Kube: kube, DryRun: true})
	byComponent := map[string][]report.Result{}
	for _, r := range results {
		byComponent[r.Component] = append(byComponent[r.Component], r)
	}
	assert.Len(t, byComponent[ComponentSecrets], len(cfg.Secrets)+len(cfg.SecretCopies))
	for _, r := range byComponent[ComponentSecrets] {
		want := report.ActionCreate
		if r.Kind == "copy" {
			want = report.ActionError // the IRIS API key is not generated
		}
		assert.Equal(t, want, r.Action, "%s %s: %s", r.Kind, r.Name, r.Detail)
	}
	for _, c := range []string{ComponentKeycloak, ComponentIRIS, "wazuh/001", "wazuh/002", ComponentVelociraptor} {
		require.Len(t, byComponent[c], 1, c)
		assert.Equal(t, report.ActionError, byComponent[c][0].Action, c)
	}
	require.Len(t, byComponent[ComponentEnrolment], len(cfg.Enrolment))
	for _, r := range byComponent[ComponentEnrolment] {
		assert.Equal(t, report.ActionError, r.Action, "the authd Secrets are sealed in, not generated")
	}
	central := map[string]report.Action{}
	for _, r := range byComponent[ComponentWazuhCentral] {
		central[r.Kind+"/"+r.Name] = r.Action
	}
	assert.Equal(t, map[string]report.Action{
		"dashboard-host/001": report.ActionError,
		"dashboard-host/002": report.ActionError,
		"dashboard-config/security-operations/wazuh-app-config": report.ActionSkip,
		"setup/credentials": report.ActionError,
	}, central)
	for _, a := range kube.Actions() {
		assert.Equal(t, "get", a.GetVerb(), "dry run only reads")
	}
}

func TestRunOnlyFilter(t *testing.T) {
	t.Parallel()
	cfg := loadExample(t)
	results := Run(context.Background(), Options{Config: cfg, Kube: fake.NewClientset(), DryRun: true, Only: []string{ComponentSecrets}})
	for _, r := range results {
		assert.Equal(t, ComponentSecrets, r.Component)
	}
	results = Run(context.Background(), Options{Config: cfg, Kube: fake.NewClientset(), DryRun: true, Only: []string{ComponentWazuh}})
	require.Len(t, results, 2)
	assert.Equal(t, "wazuh/001", results[0].Component)
	assert.Equal(t, "wazuh/002", results[1].Component)
}

func TestRunEnrolmentAndSecretCopies(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cfg := loadExample(t)
	kube := fake.NewClientset(
		secret("wazuh-001", "wazuh-authd-pass", map[string]string{"authd.pass": "pw"}),
		secret("security-operations", "iris-keycloak-sync", map[string]string{"IRIS_API_KEY": "k"}),
	)
	results := Run(ctx, Options{Config: cfg, Kube: kube, Only: []string{ComponentSecrets, ComponentEnrolment}})
	got := map[string]report.Action{}
	for _, r := range results {
		got[r.Component+"/"+r.Kind+"/"+r.Name] = r.Action
	}
	assert.Equal(t, report.ActionCreate, got["secrets/copy/wazuh-002/iris-api-key/IRIS_API_KEY"])
	assert.Equal(t, report.ActionCreate, got["enrolment/bundle/wazuh-001/enrolment-bundle"])
	assert.Equal(t, report.ActionError, got["enrolment/bundle/wazuh-002/enrolment-bundle"])
	sec, err := kube.CoreV1().Secrets("wazuh-001").Get(ctx, "iris-api-key", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "k", string(sec.Data["IRIS_API_KEY"]))
	_, err = kube.CoreV1().Secrets("wazuh-001").Get(ctx, "enrolment-bundle", metav1.GetOptions{})
	require.NoError(t, err)
}

func TestRunWazuhManagers(t *testing.T) {
	t.Parallel()
	var logins []string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/security/user/authenticate" {
			u, _, _ := r.BasicAuth()
			logins = append(logins, u)
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)
	cfg := loadExample(t)
	for i := range cfg.Components.Wazuh {
		cfg.Components.Wazuh[i].URL = srv.URL
	}
	kube := fake.NewClientset(
		secret("wazuh-001", "wazuh-api-cred", map[string]string{"API_USERNAME": "wui-001", "API_PASSWORD": "p"}),
	)
	results := Run(context.Background(), Options{Config: cfg, Kube: kube, DryRun: true, Only: []string{ComponentWazuh}})
	require.Len(t, results, 2)
	assert.Equal(t, "wazuh/001", results[0].Component)
	assert.Equal(t, "api", results[0].Kind, "manager 001 was reached with its own credentials")
	assert.Equal(t, "wazuh/002", results[1].Component)
	assert.Equal(t, "credentials", results[1].Name)
	assert.Equal(t, []string{"wui-001"}, logins)
}

func secret(ns, name string, data map[string]string) *corev1.Secret {
	s := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name}, Data: map[string][]byte{}}
	for k, v := range data {
		s.Data[k] = []byte(v)
	}
	return s
}

func TestRunIRISAndKeycloakLoginFailure(t *testing.T) {
	t.Parallel()
	irisSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data := []map[string]any{{"customer_name": "IrisInitialClient", "customer_id": 1}}
		if r.URL.Path == "/manage/groups/list" {
			data = []map[string]any{{"group_name": "Automation", "group_id": 3}}
		}
		if r.URL.Path == "/manage/users/lookup/login/svc_ai" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "error", "message": "not found"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": data})
	}))
	t.Cleanup(irisSrv.Close)
	kcSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(kcSrv.Close)

	cfg := loadExample(t)
	cfg.Components.IRIS.URL = irisSrv.URL
	cfg.Keycloak.URL = kcSrv.URL
	kube := fake.NewClientset(
		secret("security-operations", "iris-keycloak-sync", map[string]string{"IRIS_API_KEY": "k"}),
		secret("keycloakx", "keycloak-admin", map[string]string{"KEYCLOAK_PASSWORD": "p"}),
	)
	results := Run(context.Background(), Options{
		Config: cfg, Kube: kube, DryRun: true, Only: []string{ComponentKeycloak, ComponentIRIS},
	})
	got := map[string]report.Action{}
	for _, r := range results {
		got[r.Component+"/"+r.Kind+"/"+r.Name] = r.Action
	}
	assert.Equal(t, report.ActionError, got["keycloak/setup/login"])
	assert.Equal(t, report.ActionCreate, got["iris/customer/Tenant A"])
	assert.Equal(t, report.ActionSkip, got["iris/service-account/svc_ai"])
}
