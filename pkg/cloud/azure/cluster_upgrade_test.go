// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package azure

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	capzV1Beta1 "sigs.k8s.io/cluster-api-provider-azure/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
)

func TestUpdateCapiClusterValuesFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		updates any
		wantErr bool
		errMsg  string
	}{
		{
			name:    "empty NewImageOffer is a no-op",
			updates: AzureMachineTemplateUpdates{NewImageOffer: ""},
		},
		{
			name:    "wrong updates type string returns error",
			updates: "not-a-valid-type",
			wantErr: true,
			errMsg:  "wrong type of MachineTemplateUpdates object passed",
		},
		{
			name:    "wrong updates type int returns error",
			updates: 42,
			wantErr: true,
			errMsg:  "wrong type of MachineTemplateUpdates object passed",
		},
		{
			name:    "wrong updates type nil returns error",
			updates: nil,
			wantErr: true,
			errMsg:  "wrong type of MachineTemplateUpdates object passed",
		},
		{
			name:    "wrong updates type struct returns error",
			updates: struct{ Foo string }{"bar"},
			wantErr: true,
			errMsg:  "wrong type of MachineTemplateUpdates object passed",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			a := &Azure{}
			err := a.UpdateCapiClusterValuesFile(context.Background(), "/unused", tc.updates)
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errMsg)
				return
			}
			require.NoError(t, err)
		})
	}
}

// Mutates config.ParsedGeneralConfig — sequential only.
func TestUpdateMachineTemplate(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, capzV1Beta1.AddToScheme(scheme))

	const (
		clusterName = "test-cluster"
		namespace   = "capi-cluster"
	)

	existingTemplate := &capzV1Beta1.AzureMachineTemplate{
		ObjectMeta: v1.ObjectMeta{
			Name:      fmt.Sprintf("%s-control-plane", clusterName),
			Namespace: namespace,
		},
		Spec: capzV1Beta1.AzureMachineTemplateSpec{
			Template: capzV1Beta1.AzureMachineTemplateResource{
				Spec: capzV1Beta1.AzureMachineSpec{
					Image: &capzV1Beta1.Image{
						Marketplace: &capzV1Beta1.AzureMarketplaceImage{
							ImagePlan: capzV1Beta1.ImagePlan{
								Offer: "old-offer",
							},
						},
					},
				},
			},
		},
	}

	tests := []struct {
		name      string
		updates   any
		objects   []client.Object
		wantErr   bool
		errMsg    string
		wantOffer string
		noOp      bool
	}{
		{
			name:      "recreates template with new image offer",
			updates:   AzureMachineTemplateUpdates{NewImageOffer: "new-offer"},
			objects:   []client.Object{existingTemplate.DeepCopy()},
			wantOffer: "new-offer",
		},
		{
			name:    "empty NewImageOffer is a no-op",
			updates: AzureMachineTemplateUpdates{NewImageOffer: ""},
			objects: []client.Object{existingTemplate.DeepCopy()},
			noOp:    true,
		},
		{
			name:    "wrong updates type returns error",
			updates: "bad-type",
			objects: []client.Object{existingTemplate.DeepCopy()},
			wantErr: true,
			errMsg:  "wrong type of MachineTemplateUpdates object passed",
		},
		{
			name:    "missing template returns error on Get",
			updates: AzureMachineTemplateUpdates{NewImageOffer: "new-offer"},
			objects: nil,
			wantErr: true,
			errMsg:  "retrieving the current AzureMachineTemplate",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			saved := config.ParsedGeneralConfig
			t.Cleanup(func() { config.ParsedGeneralConfig = saved })

			config.ParsedGeneralConfig = &config.GeneralConfig{
				Cluster: config.ClusterConfig{Name: clusterName},
			}

			builder := fakeclient.NewClientBuilder().WithScheme(scheme)
			if len(tc.objects) > 0 {
				builder = builder.WithObjects(tc.objects...)
			}
			fakeClient := builder.Build()

			a := &Azure{}
			err := a.UpdateMachineTemplate(context.Background(), fakeClient, "", tc.updates)
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errMsg)
				return
			}
			require.NoError(t, err)

			if tc.noOp {
				return
			}

			recreated := &capzV1Beta1.AzureMachineTemplate{}
			key := client.ObjectKey{
				Name:      fmt.Sprintf("%s-control-plane", clusterName),
				Namespace: namespace,
			}
			require.NoError(t, fakeClient.Get(context.Background(), key, recreated))
			assert.Equal(t, tc.wantOffer, recreated.Spec.Template.Spec.Image.Marketplace.Offer)
		})
	}
}
