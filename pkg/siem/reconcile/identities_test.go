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
	"k8s.io/client-go/kubernetes/fake"

	"github.com/Obmondo/kubeaid-cli/pkg/siem/report"
)

const tenantA = "Tenant A"

func TestIRISSpecPerTenantAccounts(t *testing.T) {
	t.Parallel()
	is := IRISSpec(loadExample(t))
	require.Len(t, is.Groups, 1)
	assert.Equal(t, 0x48, is.Groups[0].Permissions)
	require.Len(t, is.ServiceAccounts, 3)
	assert.Nil(t, is.ServiceAccounts[0].Customers, "svc_ai keeps every tenant")
	assert.Equal(t, "svc_wazuh_001", is.ServiceAccounts[1].Login)
	assert.Equal(t, []string{tenantA}, is.ServiceAccounts[1].Customers)
	assert.True(t, is.ServiceAccounts[1].Create)
}

func TestRunMISPReadsAdminKeyAndPlans(t *testing.T) {
	t.Parallel()
	mispSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "admin-key" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		var v any
		switch r.URL.Path {
		case "/users/view/me":
			v = map[string]any{"User": map[string]any{"id": "1", "org_id": "1", "role_id": "1"}}
		case "/roles/index":
			v = []map[string]any{{"Role": map[string]any{"id": "6", "name": "Read Only"}}}
		case "/admin/users/index":
			v = []map[string]any{}
		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(v)
	}))
	t.Cleanup(mispSrv.Close)
	cfg := loadExample(t)
	cfg.Components.MISP.URL = mispSrv.URL
	kube := fake.NewClientset(secret("security-operations", "misp-api-key", map[string]string{"key": "admin-key"}))

	results := Run(context.Background(), Options{Config: cfg, Kube: kube, DryRun: true, Only: []string{ComponentMISP}})
	got := map[string]report.Action{}
	for _, r := range results {
		got[r.Component+"/"+r.Kind+"/"+r.Name] = r.Action
	}
	assert.Equal(t, report.ActionCreate, got["misp/user/ioc-export@example.com"])
	assert.Equal(t, report.ActionCreate, got["misp/user-key/ioc-export@example.com"])
}
