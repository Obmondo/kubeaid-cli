// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package iris

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-cli/pkg/siem/report"
)

const (
	intake     = "Wazuh alert intake"
	intakeMask = 0x48 // alerts_write | customers_read
	tenantA    = "Tenant A"
	tenantB    = "Tenant B"
	svcWazuhA  = "svc_wazuh_a"
)

// tenantSpec: two tenants, an owned intake group, one per-tenant
// account limited to tenant A and svc_ai with every customer.
func tenantSpec(keyA KeyStore) Spec {
	return Spec{
		Customers:       []Customer{{Name: tenantA}, {Name: tenantB}},
		InitialCustomer: initialCust,
		Groups:          []Group{{Name: intake, Description: "alerts from a tenant manager", Permissions: intakeMask}},
		ServiceAccounts: []ServiceAccount{
			{Login: svcWazuhA, Groups: []string{intake}, Create: true, Customers: []string{tenantA}, Key: keyA},
			{Login: svcLogin, Groups: []string{automation}},
		},
	}
}

func customerNames(f *fakeIRIS, ids []int) []string {
	byID := map[int]string{}
	for n, id := range f.customers {
		byID[id] = n
	}
	out := []string{}
	for _, id := range ids {
		out = append(out, byID[id])
	}
	return out
}

func TestTenantServiceAccountOnlyItsCustomer(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFakeIRIS()
	c := newClient(t, f)
	keyA := &memKeyStore{}

	dry := actions(Reconcile(ctx, c, tenantSpec(keyA), true))
	assert.Equal(t, report.ActionCreate, dry["group/"+intake])
	assert.Equal(t, report.ActionCreate, dry["service-account/"+svcWazuhA])
	assert.Equal(t, 0, f.writes, "dry run must not write")

	got := actions(Reconcile(ctx, c, tenantSpec(keyA), false))
	assert.Equal(t, report.ActionCreate, got["group/"+intake])
	assert.Equal(t, intakeMask, f.groupPerms[intake])
	assert.Equal(t, report.ActionCreate, got["service-account/"+svcWazuhA])
	assert.Equal(t, report.ActionUpdate, got["service-account-key/"+svcWazuhA])
	assert.Equal(t, svcWazuhA+"-created-key", keyA.key)

	u := f.users[svcWazuhA]
	assert.Equal(t, []int{f.groups[intake]}, u.groups)
	assert.Equal(t, []string{tenantA}, customerNames(f, u.customers), "only its own tenant, not the initial customer")
	assert.ElementsMatch(t, []string{initialCust, tenantA, tenantB}, customerNames(f, f.users[svcLogin].customers),
		"svc_ai keeps every customer")

	writes := f.writes
	for _, r := range Reconcile(ctx, c, tenantSpec(keyA), false) {
		assert.False(t, r.IsChange(), "%s %s: %s", r.Kind, r.Name, r.Detail)
	}
	assert.Equal(t, writes, f.writes, "second run must not write")
}

func TestTenantServiceAccountExtraCustomerRemoved(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFakeIRIS()
	c := newClient(t, f)
	keyA := &memKeyStore{}
	require.NotEmpty(t, Reconcile(ctx, c, tenantSpec(keyA), false))

	// Someone gives the tenant's account another tenant's customer.
	u := f.users[svcWazuhA]
	u.customers = append(u.customers, f.customers[tenantB])

	dry := Reconcile(ctx, c, tenantSpec(keyA), true)
	a := actions(dry)
	assert.Equal(t, report.ActionUpdate, a["service-account-customers/"+svcWazuhA])
	assert.Len(t, u.customers, 2, "dry run keeps it")

	got := actions(Reconcile(ctx, c, tenantSpec(keyA), false))
	assert.Equal(t, report.ActionUpdate, got["service-account-customers/"+svcWazuhA])
	assert.Equal(t, []string{tenantA}, customerNames(f, u.customers))
}

func TestOwnedGroupPermissionsReset(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFakeIRIS()
	f.nextID++
	f.groups[intake] = f.nextID
	f.groupPerms[intake] = 0xff // widened by hand
	c := newClient(t, f)

	got := Reconcile(ctx, c, Spec{Groups: []Group{{Name: intake, Permissions: intakeMask}}}, false)
	require.Len(t, got, 1)
	assert.Equal(t, report.ActionUpdate, got[0].Action)
	assert.Equal(t, "permissions 0xff -> 0x48", got[0].Detail)
	assert.Equal(t, intakeMask, f.groupPerms[intake])
}
