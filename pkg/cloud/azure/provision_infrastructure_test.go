// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package azure

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	argoCDV1Alpha1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/commandexecutor"
)

// fakeCommandExecutor implements commandexecutor.CommandExecutor for tests.
type fakeCommandExecutor struct {
	outputs map[string]string
	err     error
}

func (f *fakeCommandExecutor) Execute(_ context.Context, command string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	if out, ok := f.outputs[command]; ok {
		return out, nil
	}
	return "", nil
}

func (f *fakeCommandExecutor) MustExecute(_ context.Context, command string) string {
	out, ok := f.outputs[command]
	if !ok {
		return ""
	}
	return out
}

// Mutates syncArgoCDAppFn, newCommandExecutorFn, config.ParsedGeneralConfig — sequential only.
func TestProvisionInfrastructure(t *testing.T) {
	makeStatusCmd := func(xrClaim string) string {
		return fmt.Sprintf(`
              kubectl get %s \
                -n crossplane \
                -o "jsonpath={.status.conditions[?(@.type=='Ready')].status}"
            `, xrClaim)
	}

	tests := []struct {
		name         string
		drConfig     *config.DisasterRecoveryConfig
		syncFn       func(ctx context.Context, name string, resources []*argoCDV1Alpha1.SyncOperationResource) error
		cmdOutputs   map[string]string
		pollInterval time.Duration
		wantErr      bool
		errContains  string
	}{
		{
			name: "syncArgoCDAppFn fails",
			syncFn: func(_ context.Context, _ string, _ []*argoCDV1Alpha1.SyncOperationResource) error {
				return fmt.Errorf("infrastructure sync failed")
			},
			wantErr:     true,
			errContains: "syncing infrastructure ArgoCD app",
		},
		{
			name:     "success without disaster recovery",
			drConfig: nil,
			syncFn: func(_ context.Context, _ string, _ []*argoCDV1Alpha1.SyncOperationResource) error {
				return nil
			},
			cmdOutputs: map[string]string{
				makeStatusCmd("workloadidentityinfrastructure/default"): "True",
			},
			pollInterval: time.Millisecond,
		},
		{
			name:     "success with disaster recovery",
			drConfig: &config.DisasterRecoveryConfig{},
			syncFn: func(_ context.Context, _ string, _ []*argoCDV1Alpha1.SyncOperationResource) error {
				return nil
			},
			cmdOutputs: map[string]string{
				makeStatusCmd("workloadidentityinfrastructure/default"): "True",
				makeStatusCmd("disasterrecoveryinfrastructure/default"): "True",
			},
			pollInterval: time.Millisecond,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			savedConfig := config.ParsedGeneralConfig
			savedSync := syncArgoCDAppFn
			savedCmdFn := newCommandExecutorFn
			t.Cleanup(func() {
				config.ParsedGeneralConfig = savedConfig
				syncArgoCDAppFn = savedSync
				newCommandExecutorFn = savedCmdFn
			})

			config.ParsedGeneralConfig = &config.GeneralConfig{
				Cloud: config.CloudConfig{
					DisasterRecovery: tc.drConfig,
				},
			}

			syncArgoCDAppFn = tc.syncFn

			outputs := tc.cmdOutputs
			if outputs == nil {
				outputs = map[string]string{}
			}
			newCommandExecutorFn = func() commandexecutor.CommandExecutor {
				return &fakeCommandExecutor{outputs: outputs}
			}

			a := &Azure{pollInterval: tc.pollInterval}
			err := a.ProvisionInfrastructure(context.Background())
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errContains)
				return
			}
			require.NoError(t, err)
		})
	}
}

// Mutates newCommandExecutorFn, globals.CAPIUAMIClientID, globals.VeleroUAMIClientID,
// globals.AzureStorageAccountAccessKey, config.ParsedGeneralConfig — sequential only.
func TestGetInfrastructureDetails(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, coreV1.AddToScheme(scheme))

	secretWithKey := &coreV1.Secret{
		ObjectMeta: metaV1.ObjectMeta{
			Name:      "storage-account-details",
			Namespace: constants.NamespaceCrossPlane,
		},
		Data: map[string][]byte{
			"attribute.primary_access_key": []byte("test-access-key-123"),
		},
	}

	secretWithoutKey := &coreV1.Secret{
		ObjectMeta: metaV1.ObjectMeta{
			Name:      "storage-account-details",
			Namespace: constants.NamespaceCrossPlane,
		},
		Data: map[string][]byte{
			"other-field": []byte("value"),
		},
	}

	tests := []struct {
		name                  string
		drConfig              *config.DisasterRecoveryConfig
		objects               []client.Object
		wantCAPIClientID      string
		wantVeleroClientID    string
		wantStorageAccountKey string
		wantErr               bool
		errContains           string
	}{
		{
			name:                  "success without disaster recovery",
			objects:               []client.Object{secretWithKey.DeepCopy()},
			wantCAPIClientID:      "capi-client-id",
			wantStorageAccountKey: "test-access-key-123",
		},
		{
			name:                  "success with disaster recovery",
			drConfig:              &config.DisasterRecoveryConfig{},
			objects:               []client.Object{secretWithKey.DeepCopy()},
			wantCAPIClientID:      "capi-client-id",
			wantVeleroClientID:    "velero-client-id",
			wantStorageAccountKey: "test-access-key-123",
		},
		{
			name:        "secret not found returns error",
			wantErr:     true,
			errContains: "getting Kubernetes Secret containing storage account connection details",
		},
		{
			name:        "secret missing primary_access_key returns error",
			objects:     []client.Object{secretWithoutKey.DeepCopy()},
			wantErr:     true,
			errContains: "primary access key not found",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			savedConfig := config.ParsedGeneralConfig
			savedCmdFn := newCommandExecutorFn
			savedCAPI := globals.CAPIUAMIClientID
			savedVelero := globals.VeleroUAMIClientID
			savedStorageKey := globals.AzureStorageAccountAccessKey
			t.Cleanup(func() {
				config.ParsedGeneralConfig = savedConfig
				newCommandExecutorFn = savedCmdFn
				globals.CAPIUAMIClientID = savedCAPI
				globals.VeleroUAMIClientID = savedVelero
				globals.AzureStorageAccountAccessKey = savedStorageKey
			})

			config.ParsedGeneralConfig = &config.GeneralConfig{
				Cloud: config.CloudConfig{
					DisasterRecovery: tc.drConfig,
				},
			}

			newCommandExecutorFn = func() commandexecutor.CommandExecutor {
				return &fakeInfraDetailsExecutor{
					capiClientID:   "capi-client-id",
					veleroClientID: "velero-client-id",
				}
			}

			builder := fakeclient.NewClientBuilder().WithScheme(scheme)
			if len(tc.objects) > 0 {
				builder = builder.WithObjects(tc.objects...)
			}
			fakeClient := builder.Build()

			a := &Azure{}
			err := a.GetInfrastructureDetails(context.Background(), fakeClient)
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errContains)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantCAPIClientID, globals.CAPIUAMIClientID)
			if tc.drConfig != nil {
				assert.Equal(t, tc.wantVeleroClientID, globals.VeleroUAMIClientID)
			}
			assert.Equal(t, tc.wantStorageAccountKey, globals.AzureStorageAccountAccessKey)
		})
	}
}

// fakeInfraDetailsExecutor returns predefined client IDs based on command content.
type fakeInfraDetailsExecutor struct {
	capiClientID   string
	veleroClientID string
}

func (f *fakeInfraDetailsExecutor) Execute(_ context.Context, _ string) (string, error) {
	return "", nil
}

func (f *fakeInfraDetailsExecutor) MustExecute(_ context.Context, command string) string {
	if strings.Contains(command, `"uami=capi"`) {
		return f.capiClientID
	}
	if strings.Contains(command, `"uami=velero"`) {
		return f.veleroClientID
	}
	return ""
}
