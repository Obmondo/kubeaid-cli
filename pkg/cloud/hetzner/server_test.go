// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package hetzner

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/hetznercloud/hcloud-go/hcloud"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetHetznerBareMetalServerIP(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		serverID   string
		handler    http.HandlerFunc
		wantIP     string
		wantErr    bool
		wantErrMsg string
	}{
		{
			name:     "success returns server IP",
			serverID: "12345",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/server/12345", r.URL.Path)
				w.WriteHeader(http.StatusOK)
				_, _ = fmt.Fprint(w, `{"server":{"server_ip":"203.0.113.1"}}`)
			},
			wantIP: "203.0.113.1",
		},
		{
			name:     "HTTP error status returns error",
			serverID: "99999",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			wantErr:    true,
			wantErrMsg: "unexpected status 404",
		},
		{
			name:     "invalid JSON returns unmarshal error",
			serverID: "12345",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = fmt.Fprint(w, `not-json`)
			},
			wantErr:    true,
			wantErrMsg: "unmarshalling server 12345 response",
		},
		{
			name:     "empty server IP in valid JSON",
			serverID: "12345",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = fmt.Fprint(w, `{"server":{"server_ip":""}}`)
			},
			wantIP: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h, server := newTestHetznerWithRobotServer(tc.handler)
			defer server.Close()

			got, err := h.getHetznerBareMetalServerIP(tc.serverID)
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

func TestGetHCloudServerIDsForCluster(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		clusterName string
		listFn      func(ctx context.Context, opts hcloud.ServerListOpts) ([]*hcloud.Server, *hcloud.Response, error)
		wantIDs     []int
		wantErr     bool
		wantErrMsg  string
	}{
		{
			name:        "success returns server IDs",
			clusterName: "my-cluster",
			listFn: func(_ context.Context, _ hcloud.ServerListOpts) ([]*hcloud.Server, *hcloud.Response, error) {
				return []*hcloud.Server{
					{ID: 10},
					{ID: 20},
					{ID: 30},
				}, hcloudResponse(http.StatusOK), nil
			},
			wantIDs: []int{10, 20, 30},
		},
		{
			name:        "List returns error",
			clusterName: "my-cluster",
			listFn: func(_ context.Context, _ hcloud.ServerListOpts) ([]*hcloud.Server, *hcloud.Response, error) {
				return nil, nil, fmt.Errorf("list failed")
			},
			wantErr:    true,
			wantErrMsg: "listing HCloud servers",
		},
		{
			name:        "List returns non-OK status",
			clusterName: "my-cluster",
			listFn: func(_ context.Context, _ hcloud.ServerListOpts) ([]*hcloud.Server, *hcloud.Response, error) {
				return nil, hcloudResponse(http.StatusForbidden), nil
			},
			wantErr:    true,
			wantErrMsg: "unexpected status 403",
		},
		{
			name:        "no servers found",
			clusterName: "empty-cluster",
			listFn: func(_ context.Context, _ hcloud.ServerListOpts) ([]*hcloud.Server, *hcloud.Response, error) {
				return []*hcloud.Server{}, hcloudResponse(http.StatusOK), nil
			},
			wantIDs: []int{},
		},
		{
			name:        "single server",
			clusterName: "single",
			listFn: func(_ context.Context, opts hcloud.ServerListOpts) ([]*hcloud.Server, *hcloud.Response, error) {
				assert.Equal(t, "caph-cluster-single", opts.LabelSelector)
				return []*hcloud.Server{
					{ID: 42},
				}, hcloudResponse(http.StatusOK), nil
			},
			wantIDs: []int{42},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := &Hetzner{
				serverClient: &fakeServerClient{
					listFn: tc.listFn,
					attachToNetworkFn: func(_ context.Context, _ *hcloud.Server, _ hcloud.ServerAttachToNetworkOpts) (*hcloud.Action, *hcloud.Response, error) {
						return nil, nil, fmt.Errorf("unexpected call")
					},
				},
			}

			got, err := h.GetHCloudServerIDsForCluster(context.Background(), tc.clusterName)
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrMsg)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantIDs, got)
		})
	}
}
