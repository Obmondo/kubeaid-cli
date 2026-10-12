// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package iris

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-cli/pkg/siem/report"
)

// memKeyStore is an in-memory KeyStore.
type memKeyStore struct {
	key  string
	puts int
	err  error
}

func (m *memKeyStore) Get(context.Context) (string, error) { return m.key, m.err }

func (m *memKeyStore) Put(_ context.Context, key string, dryRun bool) (report.Action, error) {
	if key == m.key {
		return report.ActionOK, nil
	}
	if dryRun {
		return report.ActionUpdate, nil
	}
	m.key = key
	m.puts++
	return report.ActionUpdate, nil
}

func keySpec(sa ServiceAccount) Spec {
	return Spec{InitialCustomer: initialCust, ServiceAccounts: []ServiceAccount{sa}}
}

func TestServiceAccountCreatedWithKeyStored(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFakeIRIS()
	c := newClient(t, f)
	store := &memKeyStore{}
	sa := ServiceAccount{Login: "svc_new", Groups: []string{automation}, Create: true, Key: store}

	dry := actions(Reconcile(ctx, c, keySpec(sa), true))
	assert.Equal(t, report.ActionCreate, dry["service-account/svc_new"])
	assert.Equal(t, 0, f.writes, "dry run must not write")
	assert.Empty(t, store.key)

	got := actions(Reconcile(ctx, c, keySpec(sa), false))
	assert.Equal(t, report.ActionCreate, got["service-account/svc_new"])
	assert.Equal(t, report.ActionUpdate, got["service-account-key/svc_new"])
	assert.Equal(t, "svc_new-created-key", store.key)
	assert.Equal(t, []int{3}, f.users["svc_new"].groups)
	assert.Equal(t, 0, f.renewals)

	again := Reconcile(ctx, c, keySpec(sa), false)
	for _, r := range again {
		assert.Equal(t, report.ActionOK, r.Action, "%s/%s: %s", r.Kind, r.Name, r.Detail)
	}
}

func TestServiceAccountStoredKeyKept(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFakeIRIS()
	store := &memKeyStore{key: "svc-key-1"}
	sa := ServiceAccount{Login: svcLogin, Groups: []string{automation}, Key: store}

	got := actions(Reconcile(ctx, newClient(t, f), keySpec(sa), false))
	assert.Equal(t, report.ActionOK, got["service-account-key/"+svcLogin])
	assert.Equal(t, 0, f.renewals)
	assert.Equal(t, 0, store.puts)
}

func TestServiceAccountMissingOrStaleKeyRenewed(t *testing.T) {
	t.Parallel()
	for name, stored := range map[string]string{"missing": "", "stale": "revoked-key"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			f := newFakeIRIS()
			c := newClient(t, f)
			store := &memKeyStore{key: stored}
			sa := ServiceAccount{Login: svcLogin, Groups: []string{automation}, Key: store}

			dry := actions(Reconcile(ctx, c, keySpec(sa), true))
			assert.Equal(t, report.ActionUpdate, dry["service-account-key/"+svcLogin])
			assert.Equal(t, 0, f.renewals)

			got := actions(Reconcile(ctx, c, keySpec(sa), false))
			assert.Equal(t, report.ActionUpdate, got["service-account-key/"+svcLogin])
			assert.Equal(t, 1, f.renewals)
			assert.Equal(t, svcLogin+"-renewed-1", store.key)

			again := actions(Reconcile(ctx, c, keySpec(sa), false))
			assert.Equal(t, report.ActionOK, again["service-account-key/"+svcLogin])
			assert.Equal(t, 1, f.renewals)
		})
	}
}

func TestServiceAccountMissingWithoutCreate(t *testing.T) {
	t.Parallel()
	f := newFakeIRIS()
	store := &memKeyStore{}
	sa := ServiceAccount{Login: "svc_absent", Key: store}
	got := Reconcile(context.Background(), newClient(t, f), keySpec(sa), false)
	a := actions(got)
	assert.NotEqual(t, report.ActionCreate, a["service-account/svc_absent"])
	_, keyed := a["service-account-key/svc_absent"]
	assert.False(t, keyed, "no key handling for an account that does not exist")
	assert.Empty(t, store.key)
}

func TestServiceAccountKeyStoreError(t *testing.T) {
	t.Parallel()
	f := newFakeIRIS()
	store := &memKeyStore{err: errors.New("secret read failed")}
	sa := ServiceAccount{Login: svcLogin, Groups: []string{automation}, Key: store}
	got := actions(Reconcile(context.Background(), newClient(t, f), keySpec(sa), false))
	require.Equal(t, report.ActionError, got["service-account-key/"+svcLogin])
	assert.Equal(t, 0, f.renewals)
}
