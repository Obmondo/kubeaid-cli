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

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
)

type fakeNetworkClient struct {
	getFn    func(ctx context.Context, idOrName string) (*hcloud.Network, *hcloud.Response, error)
	createFn func(ctx context.Context, opts hcloud.NetworkCreateOpts) (*hcloud.Network, *hcloud.Response, error)
}

func (f *fakeNetworkClient) Get(ctx context.Context, idOrName string) (*hcloud.Network, *hcloud.Response, error) {
	return f.getFn(ctx, idOrName)
}

func (f *fakeNetworkClient) Create(ctx context.Context, opts hcloud.NetworkCreateOpts) (*hcloud.Network, *hcloud.Response, error) {
	return f.createFn(ctx, opts)
}

type fakeServerClient struct {
	attachToNetworkFn func(ctx context.Context, server *hcloud.Server, opts hcloud.ServerAttachToNetworkOpts) (*hcloud.Action, *hcloud.Response, error)
	listFn            func(ctx context.Context, opts hcloud.ServerListOpts) ([]*hcloud.Server, *hcloud.Response, error)
}

func (f *fakeServerClient) AttachToNetwork(ctx context.Context, server *hcloud.Server, opts hcloud.ServerAttachToNetworkOpts) (*hcloud.Action, *hcloud.Response, error) {
	return f.attachToNetworkFn(ctx, server, opts)
}

func (f *fakeServerClient) List(ctx context.Context, opts hcloud.ServerListOpts) ([]*hcloud.Server, *hcloud.Response, error) {
	return f.listFn(ctx, opts)
}

// Mutates config.ParsedGeneralConfig — sequential only.
func TestCreateNetwork(t *testing.T) {
	setupConfig := func(t *testing.T) {
		t.Helper()
		saved := config.ParsedGeneralConfig
		t.Cleanup(func() { config.ParsedGeneralConfig = saved })
		config.ParsedGeneralConfig = &config.GeneralConfig{
			Cluster: config.ClusterConfig{Name: "test-cluster"},
		}
	}

	existingNetwork := &hcloud.Network{ID: 10, Name: "test-cluster"}

	tests := []struct {
		name       string
		netClient  *fakeNetworkClient
		wantErr    bool
		wantErrMsg string
		wantNetID  int
	}{
		{
			name: "network already exists",
			netClient: &fakeNetworkClient{
				getFn: func(_ context.Context, _ string) (*hcloud.Network, *hcloud.Response, error) {
					return existingNetwork, hcloudResponse(http.StatusOK), nil
				},
			},
			wantNetID: 10,
		},
		{
			name: "Get returns error",
			netClient: &fakeNetworkClient{
				getFn: func(_ context.Context, _ string) (*hcloud.Network, *hcloud.Response, error) {
					return nil, nil, fmt.Errorf("network API error")
				},
			},
			wantErr:    true,
			wantErrMsg: "running Hetzner Network GET operation",
		},
		{
			name: "Get returns non-OK status",
			netClient: &fakeNetworkClient{
				getFn: func(_ context.Context, _ string) (*hcloud.Network, *hcloud.Response, error) {
					return nil, hcloudResponse(http.StatusInternalServerError), nil
				},
			},
			wantErr:    true,
			wantErrMsg: "unexpected status 500",
		},
		{
			name: "network created successfully",
			netClient: &fakeNetworkClient{
				getFn: func(_ context.Context, _ string) (*hcloud.Network, *hcloud.Response, error) {
					return nil, hcloudResponse(http.StatusOK), nil
				},
				createFn: func(_ context.Context, _ hcloud.NetworkCreateOpts) (*hcloud.Network, *hcloud.Response, error) {
					return &hcloud.Network{ID: 20, Name: "test-cluster"}, hcloudResponse(http.StatusCreated), nil
				},
			},
			wantNetID: 20,
		},
		{
			name: "Create returns error",
			netClient: &fakeNetworkClient{
				getFn: func(_ context.Context, _ string) (*hcloud.Network, *hcloud.Response, error) {
					return nil, hcloudResponse(http.StatusOK), nil
				},
				createFn: func(_ context.Context, _ hcloud.NetworkCreateOpts) (*hcloud.Network, *hcloud.Response, error) {
					return nil, nil, fmt.Errorf("create network failed")
				},
			},
			wantErr:    true,
			wantErrMsg: "creating Hetzner Network",
		},
		{
			name: "Create returns non-Created status",
			netClient: &fakeNetworkClient{
				getFn: func(_ context.Context, _ string) (*hcloud.Network, *hcloud.Response, error) {
					return nil, hcloudResponse(http.StatusOK), nil
				},
				createFn: func(_ context.Context, _ hcloud.NetworkCreateOpts) (*hcloud.Network, *hcloud.Response, error) {
					return nil, hcloudResponse(http.StatusBadRequest), nil
				},
			},
			wantErr:    true,
			wantErrMsg: "unexpected status 400",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setupConfig(t)

			h := &Hetzner{
				networkClient: tc.netClient,
			}

			got, err := h.CreateNetwork(context.Background())
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrMsg)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantNetID, got.ID)
		})
	}
}

func TestAttachHCloudServerToNetwork(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		serverID          int
		networkID         int
		attachToNetworkFn func(ctx context.Context, server *hcloud.Server, opts hcloud.ServerAttachToNetworkOpts) (*hcloud.Action, *hcloud.Response, error)
		wantErr           bool
		wantErrMsg        string
	}{
		{
			name:      "success",
			serverID:  1,
			networkID: 10,
			attachToNetworkFn: func(_ context.Context, _ *hcloud.Server, _ hcloud.ServerAttachToNetworkOpts) (*hcloud.Action, *hcloud.Response, error) {
				return &hcloud.Action{}, hcloudResponse(http.StatusCreated), nil
			},
		},
		{
			name:      "server already attached",
			serverID:  1,
			networkID: 10,
			attachToNetworkFn: func(_ context.Context, _ *hcloud.Server, _ hcloud.ServerAttachToNetworkOpts) (*hcloud.Action, *hcloud.Response, error) {
				return nil, nil, fmt.Errorf("server_already_attached")
			},
		},
		{
			name:      "other error from AttachToNetwork",
			serverID:  2,
			networkID: 20,
			attachToNetworkFn: func(_ context.Context, _ *hcloud.Server, _ hcloud.ServerAttachToNetworkOpts) (*hcloud.Action, *hcloud.Response, error) {
				return nil, nil, fmt.Errorf("some other error")
			},
			wantErr:    true,
			wantErrMsg: "attaching HCloud server 2 to Hetzner Network 20",
		},
		{
			name:      "non-Created status code",
			serverID:  3,
			networkID: 30,
			attachToNetworkFn: func(_ context.Context, _ *hcloud.Server, _ hcloud.ServerAttachToNetworkOpts) (*hcloud.Action, *hcloud.Response, error) {
				return &hcloud.Action{}, hcloudResponse(http.StatusBadRequest), nil
			},
			wantErr:    true,
			wantErrMsg: "unexpected status 400",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := &Hetzner{
				serverClient: &fakeServerClient{
					attachToNetworkFn: tc.attachToNetworkFn,
					listFn: func(_ context.Context, _ hcloud.ServerListOpts) ([]*hcloud.Server, *hcloud.Response, error) {
						return nil, nil, fmt.Errorf("unexpected call")
					},
				},
			}

			err := h.AttachHCloudServerToNetwork(context.Background(), tc.serverID, tc.networkID)
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrMsg)
				return
			}
			require.NoError(t, err)
		})
	}
}
