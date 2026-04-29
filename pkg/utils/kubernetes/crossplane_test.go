// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package kubernetes

import (
	"context"
	"errors"
	"testing"

	"github.com/argoproj/argo-cd/v2/pkg/apiclient/application"
	argoCDV1Alpha1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

type trackingArgoCDAppClient struct {
	fakeArgoCDAppClient
	getNames []string
}

func (t *trackingArgoCDAppClient) Get(ctx context.Context, q *application.ApplicationQuery, opts ...grpc.CallOption) (*argoCDV1Alpha1.Application, error) {
	if q.Name != nil {
		t.getNames = append(t.getNames, *q.Name)
	}
	return t.fakeArgoCDAppClient.Get(ctx, q, opts...)
}

func TestInstallAndSetupCrossplane(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		client        ArgoCDAppClient
		useTracking   bool
		wantErr       bool
		wantErrSubstr string
		wantGetCalled int
		wantGetNames  []string
	}{
		{
			name:        "all three apps already synced — no Sync called",
			useTracking: true,
			client: &trackingArgoCDAppClient{
				fakeArgoCDAppClient: fakeArgoCDAppClient{
					getResponses: []fakeGetResponse{{app: syncedApp(), err: nil}},
				},
			},
			wantGetCalled: 3,
			wantGetNames: []string{
				"crossplane",
				"crossplane-providers-and-functions",
				"crossplane-compositions",
			},
		},
		{
			name: "sync error on first app propagates",
			client: &fakeArgoCDAppClient{
				getResponses: []fakeGetResponse{
					{app: appWithOverallStatus(argoCDV1Alpha1.SyncStatusCodeOutOfSync), err: nil},
				},
				syncErr: errors.New("permission denied"),
			},
			wantErr:       true,
			wantErrSubstr: "failed syncing ArgoCD application",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			mgr := NewArgoCDAppManager(tc.client, nil)

			err := mgr.installAndSetupCrossplane(context.Background())
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrSubstr)
				return
			}
			require.NoError(t, err)

			if tc.useTracking {
				tracking, ok := tc.client.(*trackingArgoCDAppClient)
				require.True(t, ok)
				assert.Equal(t, tc.wantGetCalled, tracking.getCalled)
				assert.Equal(t, tc.wantGetNames, tracking.getNames)
				assert.Equal(t, 0, tracking.syncCalled)
			}
		})
	}
}
