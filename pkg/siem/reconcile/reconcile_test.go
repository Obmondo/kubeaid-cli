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

	ws := WazuhSpec(cfg)
	assert.Equal(t, []string{"tenant-001", "tenant-002"}, ws.TenantGroups)
	require.Len(t, ws.Operators, 2)
	assert.Equal(t, "oidc_administrator", ws.Operators[0].Rule)
	assert.Equal(t, "readonly", ws.Operators[1].APIRole)
	assert.True(t, ws.CreateGroups)

	vs := VelociraptorSpec(cfg)
	assert.Equal(t, []string{"Tenant A", "Tenant B"}, vs.Orgs)
	assert.Equal(t, "N", vs.Monitoring[1].Parameters["DryRun"])

	var sync config.Client
	for _, c := range cfg.Clients {
		if c.ClientID == "iris-sync" {
			sync = c
		}
	}
	cs := ClientSpec(sync, "x")
	assert.Equal(t, "x", cs.Secret)
	assert.Equal(t, []string{"query-groups", "query-users", "view-users"}, sortedCopy(cs.ScopeMappingsClient["realm-management"]))
	require.NotNil(t, cs.FullScopeAllowed)
	assert.False(t, *cs.FullScopeAllowed)
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
	assert.Len(t, byComponent[ComponentSecrets], len(cfg.Secrets))
	for _, r := range byComponent[ComponentSecrets] {
		assert.Equal(t, report.ActionCreate, r.Action)
	}
	for _, c := range []string{ComponentKeycloak, ComponentIRIS, ComponentWazuh, ComponentVelociraptor} {
		require.Len(t, byComponent[c], 1, c)
		assert.Equal(t, report.ActionError, byComponent[c][0].Action, c)
	}
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
