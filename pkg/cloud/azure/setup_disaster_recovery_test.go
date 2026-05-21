// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package azure

import (
	"context"
	"fmt"
	"testing"

	argoCDV1Alpha1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
)

// Mutates syncArgoCDAppFn, config.ParsedGeneralConfig — sequential only.
func TestSetupDisasterRecovery(t *testing.T) {
	tests := []struct {
		name        string
		drConfig    *config.DisasterRecoveryConfig
		syncFn      func(ctx context.Context, name string, resources []*argoCDV1Alpha1.SyncOperationResource) error
		wantErr     bool
		errContains string
	}{
		{
			name:        "no disaster recovery config returns error",
			drConfig:    nil,
			wantErr:     true,
			errContains: "no Azure disaster-recovery config provided",
		},
		{
			name:     "all ArgoCD syncs succeed",
			drConfig: &config.DisasterRecoveryConfig{},
			syncFn: func(_ context.Context, _ string, _ []*argoCDV1Alpha1.SyncOperationResource) error {
				return nil
			},
		},
		{
			name:     "first sync fails",
			drConfig: &config.DisasterRecoveryConfig{},
			syncFn: func(_ context.Context, name string, _ []*argoCDV1Alpha1.SyncOperationResource) error {
				if name == "azure-workload-identity-webhook" {
					return fmt.Errorf("sync timeout")
				}
				return nil
			},
			wantErr:     true,
			errContains: "syncing ArgoCD app azure-workload-identity-webhook",
		},
		{
			name:     "second sync fails",
			drConfig: &config.DisasterRecoveryConfig{},
			syncFn: func(_ context.Context, name string, _ []*argoCDV1Alpha1.SyncOperationResource) error {
				if name == "velero" {
					return fmt.Errorf("velero sync failed")
				}
				return nil
			},
			wantErr:     true,
			errContains: "syncing ArgoCD app velero",
		},
		{
			name:     "third sync fails",
			drConfig: &config.DisasterRecoveryConfig{},
			syncFn: func(_ context.Context, name string, _ []*argoCDV1Alpha1.SyncOperationResource) error {
				if name == "sealed-secrets" {
					return fmt.Errorf("sealed-secrets sync failed")
				}
				return nil
			},
			wantErr:     true,
			errContains: "syncing ArgoCD app sealed-secrets",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			savedConfig := config.ParsedGeneralConfig
			savedSync := syncArgoCDAppFn
			t.Cleanup(func() {
				config.ParsedGeneralConfig = savedConfig
				syncArgoCDAppFn = savedSync
			})

			config.ParsedGeneralConfig = &config.GeneralConfig{
				Cloud: config.CloudConfig{
					DisasterRecovery: tc.drConfig,
				},
			}

			if tc.syncFn != nil {
				syncArgoCDAppFn = tc.syncFn
			}

			a := &Azure{}
			err := a.SetupDisasterRecovery(context.Background())
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errContains)
				return
			}
			require.NoError(t, err)
		})
	}
}
