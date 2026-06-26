// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package hetzner

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"testing"

	"github.com/hetznercloud/hcloud-go/hcloud"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeFloatingIPClient struct {
	getByNameFn        func(ctx context.Context, name string) (*hcloud.FloatingIP, *hcloud.Response, error)
	createFn           func(ctx context.Context, opts hcloud.FloatingIPCreateOpts) (hcloud.FloatingIPCreateResult, *hcloud.Response, error)
	changeProtectionFn func(ctx context.Context, floatingIP *hcloud.FloatingIP, opts hcloud.FloatingIPChangeProtectionOpts) (*hcloud.Action, *hcloud.Response, error)
}

func (f *fakeFloatingIPClient) GetByName(ctx context.Context, name string) (*hcloud.FloatingIP, *hcloud.Response, error) {
	return f.getByNameFn(ctx, name)
}

func (f *fakeFloatingIPClient) Create(ctx context.Context, opts hcloud.FloatingIPCreateOpts) (hcloud.FloatingIPCreateResult, *hcloud.Response, error) {
	return f.createFn(ctx, opts)
}

func (f *fakeFloatingIPClient) ChangeProtection(ctx context.Context, floatingIP *hcloud.FloatingIP, opts hcloud.FloatingIPChangeProtectionOpts) (*hcloud.Action, *hcloud.Response, error) {
	if f.changeProtectionFn != nil {
		return f.changeProtectionFn(ctx, floatingIP, opts)
	}
	return nil, hcloudResponse(http.StatusCreated), nil
}

// TestCreateCoturnFloatingIP covers the bootstrap-state cases operators
// actually hit: the IP already exists (re-run reuses it, Create never
// fires), a fresh allocate-and-protect, and every API failure path.
func TestCreateCoturnFloatingIP(t *testing.T) {
	t.Parallel()

	const clusterName = "test-cluster"

	tests := []struct {
		name       string
		client     *fakeFloatingIPClient
		wantIP     string
		wantErr    bool
		wantErrMsg string
	}{
		{
			name: "existing Floating IP is reused without creating a new one",
			client: &fakeFloatingIPClient{
				getByNameFn: func(_ context.Context, _ string) (*hcloud.FloatingIP, *hcloud.Response, error) {
					return &hcloud.FloatingIP{IP: net.ParseIP("203.0.113.10")}, hcloudResponse(http.StatusOK), nil
				},
				createFn: func(_ context.Context, _ hcloud.FloatingIPCreateOpts) (hcloud.FloatingIPCreateResult, *hcloud.Response, error) {
					t.Error("Create must not be called when the Floating IP already exists")
					return hcloud.FloatingIPCreateResult{}, nil, nil
				},
			},
			wantIP: "203.0.113.10",
		},
		{
			name: "fresh Floating IP is created and deletion-protected",
			client: &fakeFloatingIPClient{
				getByNameFn: func(_ context.Context, _ string) (*hcloud.FloatingIP, *hcloud.Response, error) {
					return nil, hcloudResponse(http.StatusOK), nil
				},
				createFn: func(_ context.Context, _ hcloud.FloatingIPCreateOpts) (hcloud.FloatingIPCreateResult, *hcloud.Response, error) {
					return hcloud.FloatingIPCreateResult{
						FloatingIP: &hcloud.FloatingIP{IP: net.ParseIP("203.0.113.20")},
					}, hcloudResponse(http.StatusCreated), nil
				},
			},
			wantIP: "203.0.113.20",
		},
		{
			name: "GetByName error surfaces",
			client: &fakeFloatingIPClient{
				getByNameFn: func(_ context.Context, _ string) (*hcloud.FloatingIP, *hcloud.Response, error) {
					return nil, nil, fmt.Errorf("network timeout")
				},
			},
			wantErr:    true,
			wantErrMsg: "checking for existing Coturn Floating IP",
		},
		{
			name: "GetByName unexpected status surfaces",
			client: &fakeFloatingIPClient{
				getByNameFn: func(_ context.Context, _ string) (*hcloud.FloatingIP, *hcloud.Response, error) {
					return nil, hcloudResponse(http.StatusInternalServerError), nil
				},
			},
			wantErr:    true,
			wantErrMsg: "unexpected status 500",
		},
		{
			name: "Create error surfaces",
			client: &fakeFloatingIPClient{
				getByNameFn: func(_ context.Context, _ string) (*hcloud.FloatingIP, *hcloud.Response, error) {
					return nil, hcloudResponse(http.StatusOK), nil
				},
				createFn: func(_ context.Context, _ hcloud.FloatingIPCreateOpts) (hcloud.FloatingIPCreateResult, *hcloud.Response, error) {
					return hcloud.FloatingIPCreateResult{}, nil, fmt.Errorf("create failed")
				},
			},
			wantErr:    true,
			wantErrMsg: "creating Coturn Floating IP",
		},
		{
			name: "Create nil response surfaces",
			client: &fakeFloatingIPClient{
				getByNameFn: func(_ context.Context, _ string) (*hcloud.FloatingIP, *hcloud.Response, error) {
					return nil, hcloudResponse(http.StatusOK), nil
				},
				createFn: func(_ context.Context, _ hcloud.FloatingIPCreateOpts) (hcloud.FloatingIPCreateResult, *hcloud.Response, error) {
					return hcloud.FloatingIPCreateResult{}, nil, nil
				},
			},
			wantErr:    true,
			wantErrMsg: "nil response",
		},
		{
			name: "Create unexpected status surfaces",
			client: &fakeFloatingIPClient{
				getByNameFn: func(_ context.Context, _ string) (*hcloud.FloatingIP, *hcloud.Response, error) {
					return nil, hcloudResponse(http.StatusOK), nil
				},
				createFn: func(_ context.Context, _ hcloud.FloatingIPCreateOpts) (hcloud.FloatingIPCreateResult, *hcloud.Response, error) {
					return hcloud.FloatingIPCreateResult{}, hcloudResponse(http.StatusBadRequest), nil
				},
			},
			wantErr:    true,
			wantErrMsg: "unexpected status 400",
		},
		{
			name: "ChangeProtection error surfaces",
			client: &fakeFloatingIPClient{
				getByNameFn: func(_ context.Context, _ string) (*hcloud.FloatingIP, *hcloud.Response, error) {
					return nil, hcloudResponse(http.StatusOK), nil
				},
				createFn: func(_ context.Context, _ hcloud.FloatingIPCreateOpts) (hcloud.FloatingIPCreateResult, *hcloud.Response, error) {
					return hcloud.FloatingIPCreateResult{
						FloatingIP: &hcloud.FloatingIP{IP: net.ParseIP("203.0.113.30")},
					}, hcloudResponse(http.StatusCreated), nil
				},
				changeProtectionFn: func(_ context.Context, _ *hcloud.FloatingIP, _ hcloud.FloatingIPChangeProtectionOpts) (*hcloud.Action, *hcloud.Response, error) {
					return nil, nil, fmt.Errorf("protection failed")
				},
			},
			wantErr:    true,
			wantErrMsg: "enabling deletion protection on Coturn Floating IP",
		},
		{
			name: "ChangeProtection unexpected status surfaces",
			client: &fakeFloatingIPClient{
				getByNameFn: func(_ context.Context, _ string) (*hcloud.FloatingIP, *hcloud.Response, error) {
					return nil, hcloudResponse(http.StatusOK), nil
				},
				createFn: func(_ context.Context, _ hcloud.FloatingIPCreateOpts) (hcloud.FloatingIPCreateResult, *hcloud.Response, error) {
					return hcloud.FloatingIPCreateResult{
						FloatingIP: &hcloud.FloatingIP{IP: net.ParseIP("203.0.113.40")},
					}, hcloudResponse(http.StatusCreated), nil
				},
				changeProtectionFn: func(_ context.Context, _ *hcloud.FloatingIP, _ hcloud.FloatingIPChangeProtectionOpts) (*hcloud.Action, *hcloud.Response, error) {
					return nil, hcloudResponse(http.StatusBadRequest), nil
				},
			},
			wantErr:    true,
			wantErrMsg: "unexpected status 400",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := &Hetzner{floatingIPClient: tc.client}
			got, err := h.CreateCoturnFloatingIP(context.Background(), clusterName, "hel1")
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrMsg)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantIP, got)
		})
	}
}

func TestCoturnFloatingIPName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		clusterName string
		want        string
	}{
		{"standard cluster name", "prod", "prod-coturn"},
		{"empty cluster name", "", "-coturn"},
		{"cluster name with hyphens", "my-vpn-cluster", "my-vpn-cluster-coturn"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, coturnFloatingIPName(tc.clusterName))
		})
	}
}

func TestCoturnFloatingIPOwnershipLabel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		clusterName string
		want        string
	}{
		{"standard cluster name", "prod", "caph-cluster-prod"},
		{"empty cluster name", "", "caph-cluster-"},
		{"cluster name with hyphens", "my-vpn-cluster", "caph-cluster-my-vpn-cluster"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, coturnFloatingIPOwnershipLabel(tc.clusterName))
		})
	}
}
