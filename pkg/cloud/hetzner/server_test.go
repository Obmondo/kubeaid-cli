// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package hetzner

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/hetznercloud/hcloud-go/hcloud"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPrivateIPOnNetwork pins down the pure helper that resolves the
// server's private IP within a specific Hetzner network. Regression
// test for the panic that fired on a fresh re-run after the operator
// deleted the NAT gateway from the Hetzner console: the previous
// code blindly indexed `server.PrivateNet[0].IP` and crashed when
// hcloud's Server.GetByName / Server.Create response came back with
// the attachment not yet populated.
func TestPrivateIPOnNetwork(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		server    *hcloud.Server
		networkID int
		wantIP    net.IP
	}{
		{
			name: "returns IP for matching network",
			server: &hcloud.Server{
				PrivateNet: []hcloud.ServerPrivateNet{
					{Network: &hcloud.Network{ID: 42}, IP: net.IPv4(10, 0, 0, 2)},
				},
			},
			networkID: 42,
			wantIP:    net.IPv4(10, 0, 0, 2),
		},
		{
			name: "returns nil when PrivateNet is empty (the panic case)",
			server: &hcloud.Server{
				PrivateNet: nil,
			},
			networkID: 42,
			wantIP:    nil,
		},
		{
			name: "returns nil when no attachment matches the network",
			server: &hcloud.Server{
				PrivateNet: []hcloud.ServerPrivateNet{
					{Network: &hcloud.Network{ID: 99}, IP: net.IPv4(10, 99, 0, 2)},
				},
			},
			networkID: 42,
			wantIP:    nil,
		},
		{
			name: "picks the matching network when multiple attachments exist",
			server: &hcloud.Server{
				PrivateNet: []hcloud.ServerPrivateNet{
					{Network: &hcloud.Network{ID: 99}, IP: net.IPv4(10, 99, 0, 2)},
					{Network: &hcloud.Network{ID: 42}, IP: net.IPv4(10, 0, 0, 2)},
				},
			},
			networkID: 42,
			wantIP:    net.IPv4(10, 0, 0, 2),
		},
		{
			name: "ignores entries with nil Network",
			server: &hcloud.Server{
				PrivateNet: []hcloud.ServerPrivateNet{
					{Network: nil, IP: net.IPv4(10, 0, 0, 3)},
					{Network: &hcloud.Network{ID: 42}, IP: net.IPv4(10, 0, 0, 2)},
				},
			},
			networkID: 42,
			wantIP:    net.IPv4(10, 0, 0, 2),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := privateIPOnNetwork(tc.server, tc.networkID)
			assert.True(t, got.Equal(tc.wantIP),
				"got %v, want %v", got, tc.wantIP)
		})
	}
}

// TestEnsureServerAttachedToNetwork exercises the poll-then-attach
// fallback that ensureNATRouteOnNetwork relies on. Mirrors the
// scenarios that cause the original `server.PrivateNet[0]` panic.
func TestEnsureServerAttachedToNetwork(t *testing.T) {
	// Drives the GetByID poll cadence. With the real 2s tick this
	// test would be slow; injecting a shorter sleep makes the poll
	// loop deterministic in unit time.
	//
	// Mutates h.sleepFunc — sequential only.

	const networkID = 42
	matchingIP := net.IPv4(10, 0, 0, 2)

	tests := []struct {
		name         string
		inputServer  *hcloud.Server
		client       *fakeServerClient
		wantIP       net.IP
		wantErrMsg   string
		wantGetCalls int // expected GetByID invocations
	}{
		{
			name: "already attached on input — no API calls",
			inputServer: &hcloud.Server{
				ID: 1,
				PrivateNet: []hcloud.ServerPrivateNet{
					{Network: &hcloud.Network{ID: networkID}, IP: matchingIP},
				},
			},
			client:       &fakeServerClient{},
			wantIP:       matchingIP,
			wantGetCalls: 0,
		},
		{
			name: "attachment shows up on first GetByID poll",
			inputServer: &hcloud.Server{ID: 1, PrivateNet: nil},
			client: func() *fakeServerClient {
				return &fakeServerClient{
					getByIDFn: func(_ context.Context, id int) (*hcloud.Server, *hcloud.Response, error) {
						return &hcloud.Server{
							ID: id,
							PrivateNet: []hcloud.ServerPrivateNet{
								{Network: &hcloud.Network{ID: networkID}, IP: matchingIP},
							},
						}, nil, nil
					},
				}
			}(),
			wantIP:       matchingIP,
			wantGetCalls: 1,
		},
		{
			name:        "GetByID returns nil — server vanished",
			inputServer: &hcloud.Server{ID: 7, PrivateNet: nil},
			client: &fakeServerClient{
				getByIDFn: func(_ context.Context, _ int) (*hcloud.Server, *hcloud.Response, error) {
					return nil, nil, nil
				},
			},
			wantErrMsg:   "not found on re-fetch",
			wantGetCalls: 1,
		},
		{
			name:        "GetByID errors — surfaces wrapped error",
			inputServer: &hcloud.Server{ID: 7, PrivateNet: nil},
			client: &fakeServerClient{
				getByIDFn: func(_ context.Context, _ int) (*hcloud.Server, *hcloud.Response, error) {
					return nil, nil, fmt.Errorf("api unavailable")
				},
			},
			wantErrMsg:   "re-fetching NAT gateway server",
			wantGetCalls: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Track GetByID invocations by wrapping the user's fn.
			var calls int
			origGetByID := tc.client.getByIDFn
			tc.client.getByIDFn = func(ctx context.Context, id int) (*hcloud.Server, *hcloud.Response, error) {
				calls++
				if origGetByID == nil {
					return nil, nil, nil
				}
				return origGetByID(ctx, id)
			}

			h := &Hetzner{
				serverClient: tc.client,
				sleepFunc:    func(time.Duration) {}, // skip 2s polling tick
			}

			// Tight per-test deadline so the loop doesn't burn the
			// real 30s ceiling when a case expects to give up.
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()

			gotIP, err := h.ensureServerAttachedToNetwork(ctx, tc.inputServer, networkID)

			if tc.wantErrMsg != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrMsg)
			} else {
				require.NoError(t, err)
				assert.True(t, gotIP.Equal(tc.wantIP),
					"got %v, want %v", gotIP, tc.wantIP)
			}
			assert.Equal(t, tc.wantGetCalls, calls, "GetByID call count")
		})
	}
}

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
