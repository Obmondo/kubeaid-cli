// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package keycloak

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReconcileNetBird_RequiredFields(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		spec NetBirdSpec
		want string
	}{
		{
			name: "missing realm",
			spec: NetBirdSpec{
				VPNClusterName:       "acme-vpn",
				NetBirdMgmtURL:       "https://nb.acme.com",
				NetBirdBackendSecret: "s",
			},
			want: "spec.Realm",
		},
		{
			name: "missing vpn cluster name",
			spec: NetBirdSpec{
				Realm:                "acme",
				NetBirdMgmtURL:       "https://nb.acme.com",
				NetBirdBackendSecret: "s",
			},
			want: "spec.VPNClusterName",
		},
		{
			name: "missing mgmt URL",
			spec: NetBirdSpec{
				Realm:                "acme",
				VPNClusterName:       "acme-vpn",
				NetBirdBackendSecret: "s",
			},
			want: "spec.NetBirdMgmtURL",
		},
		{
			name: "missing backend secret",
			spec: NetBirdSpec{
				Realm:          "acme",
				VPNClusterName: "acme-vpn",
				NetBirdMgmtURL: "https://nb.acme.com",
			},
			want: "spec.NetBirdBackendSecret",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := newTestReconciler(t)
			err := r.ReconcileNetBird(context.Background(), tc.spec)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestReconcileNetBird_HappyPathAndIdempotent(t *testing.T) {
	t.Parallel()

	r, fake := newTestReconciler(t)

	// realm-management is a Keycloak built-in that the real server
	// auto-creates per realm; the fake doesn't, so seed it here as
	// a plain confidential client (the fake's clientRolesFixture
	// supplies its view-users role keyed on this clientId).
	require.NoError(t, r.ReconcileRealm(context.Background(), "acme"))
	_, err := r.ReconcileClient(context.Background(), "acme", ClientSpec{
		ClientID:     realmManagementClientID,
		PublicClient: false,
	})
	require.NoError(t, err)
	beforeOrch := fake.writeCount

	spec := NetBirdSpec{
		Realm:                "acme",
		VPNClusterName:       "acme-vpn",
		NetBirdMgmtURL:       "https://nb.acme.com",
		NetBirdBackendSecret: "pre-generated-secret-from-kubeaid-cli",
	}

	require.NoError(t, r.ReconcileNetBird(context.Background(), spec))
	firstRunWrites := fake.writeCount - beforeOrch

	require.NoError(t, r.ReconcileNetBird(context.Background(), spec))
	assert.Equal(t, firstRunWrites, fake.writeCount-beforeOrch,
		"second ReconcileNetBird must not write anything new")
}

func TestReconcileNetBird_BackendSecretIsHonored(t *testing.T) {
	t.Parallel()

	const preSet = "pre-generated-secret-from-kubeaid-cli"

	r, fake := newTestReconciler(t)
	require.NoError(t, r.ReconcileRealm(context.Background(), "acme"))
	_, err := r.ReconcileClient(context.Background(), "acme", ClientSpec{
		ClientID:     realmManagementClientID,
		PublicClient: false,
	})
	require.NoError(t, err)

	require.NoError(t, r.ReconcileNetBird(context.Background(), NetBirdSpec{
		Realm:                "acme",
		VPNClusterName:       "acme-vpn",
		NetBirdMgmtURL:       "https://nb.acme.com",
		NetBirdBackendSecret: preSet,
	}))

	// Verify the netbird-backend client landed in the fake with
	// the pre-set secret (rather than a generated one).
	for _, c := range fake.clients["acme"] {
		if c.ClientID != nil && *c.ClientID == "netbird-backend" {
			require.NotNil(t, c.Secret)
			assert.Equal(t, preSet, *c.Secret)
			return
		}
	}
	t.Fatalf("netbird-backend client not found")
}

func TestReconcileNetBird_KubernetesClientUsesVPNClusterName(t *testing.T) {
	t.Parallel()

	r, fake := newTestReconciler(t)
	require.NoError(t, r.ReconcileRealm(context.Background(), "acme"))
	_, err := r.ReconcileClient(context.Background(), "acme", ClientSpec{
		ClientID:     realmManagementClientID,
		PublicClient: false,
	})
	require.NoError(t, err)

	require.NoError(t, r.ReconcileNetBird(context.Background(), NetBirdSpec{
		Realm:                "acme",
		VPNClusterName:       "acme-vpn",
		NetBirdMgmtURL:       "https://nb.acme.com",
		NetBirdBackendSecret: "x",
	}))

	want := "kubernetes-acme-vpn"
	for _, c := range fake.clients["acme"] {
		if c.ClientID != nil && *c.ClientID == want {
			return
		}
	}
	t.Fatalf("expected client %q in realm 'acme'", want)
}
