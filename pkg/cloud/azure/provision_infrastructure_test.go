// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package azure

import (
	"context"
	"fmt"
	"testing"
	"time"

	argoCDV1Alpha1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
)

func readyXRClaim(kind, name string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"status": map[string]interface{}{
				"conditions": []interface{}{
					map[string]interface{}{
						"type":   "Ready",
						"status": "True",
					},
				},
			},
		},
	}
	obj.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "infrastructure.obmondo.com",
		Version: "v1alpha1",
		Kind:    kind,
	})
	obj.SetName(name)
	obj.SetNamespace(constants.NamespaceCrossPlane)
	return obj
}

func roleAssignment(name, uami string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "authorization.azure.upbound.io",
		Version: "v1beta1",
		Kind:    "RoleAssignment",
	})
	obj.SetName(name)
	obj.SetLabels(map[string]string{"uami": uami})
	return obj
}

func userAssignedIdentity(name, uami, clientID string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"status": map[string]interface{}{
				"atProvider": map[string]interface{}{
					"clientId": clientID,
				},
			},
		},
	}
	obj.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "managedidentity.azure.upbound.io",
		Version: "v1beta1",
		Kind:    "UserAssignedIdentity",
	})
	obj.SetName(name)
	obj.SetNamespace(constants.NamespaceCrossPlane)
	obj.SetLabels(map[string]string{"uami": uami})
	return obj
}

// Mutates syncArgoCDAppFn, createUnstructuredClientFn, config.ParsedGeneralConfig — sequential only.
func TestProvisionInfrastructure(t *testing.T) {
	tests := []struct {
		name         string
		drConfig     *config.DisasterRecoveryConfig
		syncFn       func(ctx context.Context, name string, resources []*argoCDV1Alpha1.SyncOperationResource) error
		objects      []client.Object
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
			objects: []client.Object{
				readyXRClaim("WorkloadIdentityInfrastructure", "default"),
				roleAssignment("capi-role-assignment", "capi"),
			},
			pollInterval: time.Millisecond,
		},
		{
			name:     "success with disaster recovery",
			drConfig: &config.DisasterRecoveryConfig{},
			syncFn: func(_ context.Context, _ string, _ []*argoCDV1Alpha1.SyncOperationResource) error {
				return nil
			},
			objects: []client.Object{
				readyXRClaim("WorkloadIdentityInfrastructure", "default"),
				readyXRClaim("DisasterRecoveryInfrastructure", "default"),
				roleAssignment("capi-role-assignment", "capi"),
				roleAssignment("velero-role-assignment", "velero"),
			},
			pollInterval: time.Millisecond,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			savedConfig := config.ParsedGeneralConfig
			savedSync := syncArgoCDAppFn
			savedClientFn := createUnstructuredClientFn
			t.Cleanup(func() {
				config.ParsedGeneralConfig = savedConfig
				syncArgoCDAppFn = savedSync
				createUnstructuredClientFn = savedClientFn
			})

			config.ParsedGeneralConfig = &config.GeneralConfig{
				Cloud: config.CloudConfig{
					DisasterRecovery: tc.drConfig,
				},
			}

			syncArgoCDAppFn = tc.syncFn

			builder := fakeclient.NewClientBuilder().WithScheme(runtime.NewScheme())
			if len(tc.objects) > 0 {
				builder = builder.WithObjects(tc.objects...)
			}
			fakeClient := builder.Build()
			createUnstructuredClientFn = func(context.Context) (client.Client, error) {
				return fakeClient, nil
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

// Mutates createUnstructuredClientFn, globals.CAPIUAMIClientID, globals.VeleroUAMIClientID,
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
			name: "success without disaster recovery",
			objects: []client.Object{
				secretWithKey.DeepCopy(),
				userAssignedIdentity("capi-uami", "capi", "capi-client-id"),
			},
			wantCAPIClientID:      "capi-client-id",
			wantStorageAccountKey: "test-access-key-123",
		},
		{
			name:     "success with disaster recovery",
			drConfig: &config.DisasterRecoveryConfig{},
			objects: []client.Object{
				secretWithKey.DeepCopy(),
				userAssignedIdentity("capi-uami", "capi", "capi-client-id"),
				userAssignedIdentity("velero-uami", "velero", "velero-client-id"),
			},
			wantCAPIClientID:      "capi-client-id",
			wantVeleroClientID:    "velero-client-id",
			wantStorageAccountKey: "test-access-key-123",
		},
		{
			name: "secret not found returns error",
			objects: []client.Object{
				userAssignedIdentity("capi-uami", "capi", "capi-client-id"),
			},
			wantErr:     true,
			errContains: "getting Kubernetes Secret containing storage account connection details",
		},
		{
			name: "secret missing primary_access_key returns error",
			objects: []client.Object{
				secretWithoutKey.DeepCopy(),
				userAssignedIdentity("capi-uami", "capi", "capi-client-id"),
			},
			wantErr:     true,
			errContains: "primary access key not found",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			savedConfig := config.ParsedGeneralConfig
			savedClientFn := createUnstructuredClientFn
			savedCAPI := globals.CAPIUAMIClientID
			savedVelero := globals.VeleroUAMIClientID
			savedStorageKey := globals.AzureStorageAccountAccessKey
			t.Cleanup(func() {
				config.ParsedGeneralConfig = savedConfig
				createUnstructuredClientFn = savedClientFn
				globals.CAPIUAMIClientID = savedCAPI
				globals.VeleroUAMIClientID = savedVelero
				globals.AzureStorageAccountAccessKey = savedStorageKey
			})

			config.ParsedGeneralConfig = &config.GeneralConfig{
				Cloud: config.CloudConfig{
					DisasterRecovery: tc.drConfig,
				},
			}

			builder := fakeclient.NewClientBuilder().WithScheme(scheme)
			if len(tc.objects) > 0 {
				builder = builder.WithObjects(tc.objects...)
			}
			fakeClient := builder.Build()
			createUnstructuredClientFn = func(context.Context) (client.Client, error) {
				return fakeClient, nil
			}

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
