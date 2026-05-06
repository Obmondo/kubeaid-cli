// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package hetzner

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/hetznercloud/hcloud-go/hcloud"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud"
)

type fakeServerTypeClient struct {
	getByNameFn func(ctx context.Context, name string) (*hcloud.ServerType, *hcloud.Response, error)
}

func (f *fakeServerTypeClient) GetByName(ctx context.Context, name string) (*hcloud.ServerType, *hcloud.Response, error) {
	return f.getByNameFn(ctx, name)
}

func hcloudResponse(statusCode int) *hcloud.Response {
	return &hcloud.Response{Response: &http.Response{StatusCode: statusCode}}
}

func TestGetVMSpecs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		machineType string
		getByNameFn func(ctx context.Context, name string) (*hcloud.ServerType, *hcloud.Response, error)
		want        *cloud.VMSpec
		wantErr     bool
		wantErrMsg  string
	}{
		{
			name:        "success returns correct specs",
			machineType: "cpx31",
			getByNameFn: func(_ context.Context, name string) (*hcloud.ServerType, *hcloud.Response, error) {
				return &hcloud.ServerType{
					Name:   name,
					Cores:  4,
					Memory: 8,
					Disk:   160,
				}, hcloudResponse(http.StatusOK), nil
			},
			want: &cloud.VMSpec{
				CPU:            4,
				Memory:         8,
				RootVolumeSize: aws.Uint32(160),
			},
		},
		{
			name:        "error from GetByName",
			machineType: "invalid-type",
			getByNameFn: func(_ context.Context, _ string) (*hcloud.ServerType, *hcloud.Response, error) {
				return nil, nil, fmt.Errorf("API error")
			},
			wantErr:    true,
			wantErrMsg: "getting machine details",
		},
		{
			name:        "nil server type returned",
			machineType: "nonexistent",
			getByNameFn: func(_ context.Context, _ string) (*hcloud.ServerType, *hcloud.Response, error) {
				return nil, hcloudResponse(http.StatusOK), nil
			},
			wantErr:    true,
			wantErrMsg: "not found",
		},
		{
			name:        "zero cores and memory",
			machineType: "cpx-zero",
			getByNameFn: func(_ context.Context, name string) (*hcloud.ServerType, *hcloud.Response, error) {
				return &hcloud.ServerType{
					Name:   name,
					Cores:  0,
					Memory: 0,
					Disk:   0,
				}, hcloudResponse(http.StatusOK), nil
			},
			want: &cloud.VMSpec{
				CPU:            0,
				Memory:         0,
				RootVolumeSize: aws.Uint32(0),
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := &Hetzner{
				serverTypeClient: &fakeServerTypeClient{getByNameFn: tc.getByNameFn},
			}

			got, err := h.GetVMSpecs(context.Background(), tc.machineType)
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrMsg)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
