// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package aws

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	capaV1Beta2 "sigs.k8s.io/cluster-api-provider-aws/v2/api/v1beta2"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
)

// Mutates config.ParsedGeneralConfig — sequential only.
func TestUpdateMachineTemplate(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, capaV1Beta2.AddToScheme(scheme))

	oldAMI := "ami-old-123"
	existingTemplate := &capaV1Beta2.AWSMachineTemplate{
		ObjectMeta: v1.ObjectMeta{
			Name:            "my-template",
			Namespace:       "capi-cluster",
			ResourceVersion: "999",
		},
		Spec: capaV1Beta2.AWSMachineTemplateSpec{
			Template: capaV1Beta2.AWSMachineTemplateResource{
				Spec: capaV1Beta2.AWSMachineSpec{
					AMI: capaV1Beta2.AMIReference{
						ID: &oldAMI,
					},
				},
			},
		},
	}

	tests := []struct {
		name      string
		setup     func(t *testing.T) *fakeclient.ClientBuilder
		updates   any
		wantErr   bool
		errMsg    string
		wantAMIID string
	}{
		{
			name: "wrong updates type returns error",
			setup: func(_ *testing.T) *fakeclient.ClientBuilder {
				return fakeclient.NewClientBuilder().WithScheme(scheme)
			},
			updates: "not-a-valid-type",
			wantErr: true,
			errMsg:  "wrong type of MachineTemplateUpdates object passed",
		},
		{
			name: "wrong updates type nil returns error",
			setup: func(_ *testing.T) *fakeclient.ClientBuilder {
				return fakeclient.NewClientBuilder().WithScheme(scheme)
			},
			updates: nil,
			wantErr: true,
			errMsg:  "wrong type of MachineTemplateUpdates object passed",
		},
		{
			name: "get fails when template does not exist",
			setup: func(_ *testing.T) *fakeclient.ClientBuilder {
				return fakeclient.NewClientBuilder().WithScheme(scheme)
			},
			updates: AWSMachineTemplateUpdates{AMIID: "ami-new-456"},
			wantErr: true,
			errMsg:  "retrieving the current AWSMachineTemplate",
		},
		{
			name: "success replaces template with new AMI",
			setup: func(_ *testing.T) *fakeclient.ClientBuilder {
				return fakeclient.NewClientBuilder().WithScheme(scheme).WithObjects(existingTemplate.DeepCopy())
			},
			updates:   AWSMachineTemplateUpdates{AMIID: "ami-new-456"},
			wantAMIID: "ami-new-456",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			saved := config.ParsedGeneralConfig
			t.Cleanup(func() { config.ParsedGeneralConfig = saved })
			config.ParsedGeneralConfig = &config.GeneralConfig{}

			cl := tc.setup(t).Build()

			a := &AWS{}
			err := a.UpdateMachineTemplate(context.Background(), cl, "my-template", tc.updates)

			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errMsg)
				return
			}
			require.NoError(t, err)

			recreated := &capaV1Beta2.AWSMachineTemplate{}
			require.NoError(t, cl.Get(context.Background(),
				types.NamespacedName{Name: "my-template", Namespace: "capi-cluster"},
				recreated))
			require.NotNil(t, recreated.Spec.Template.Spec.AMI.ID)
			assert.Equal(t, tc.wantAMIID, *recreated.Spec.Template.Spec.AMI.ID)
		})
	}
}

// Mutates yq package-level globals via yqCmdLib.New() — sequential only.
func TestUpdateCapiClusterValuesFile(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T) string
		updates any
		wantErr bool
		errMsg  string
	}{
		{
			name: "wrong updates type returns error",
			setup: func(_ *testing.T) string {
				return "/unused"
			},
			updates: "not-a-valid-type",
			wantErr: true,
			errMsg:  "wrong type of MachineTemplateUpdates object passed",
		},
		{
			name: "wrong updates type nil returns error",
			setup: func(_ *testing.T) string {
				return "/unused"
			},
			updates: nil,
			wantErr: true,
			errMsg:  "wrong type of MachineTemplateUpdates object passed",
		},
		{
			name: "returns error for nonexistent file",
			setup: func(t *testing.T) string {
				t.Helper()
				return filepath.Join(t.TempDir(), "nonexistent.yaml")
			},
			updates: AWSMachineTemplateUpdates{AMIID: "ami-new-999"},
			wantErr: true,
			errMsg:  "updating AMI ID for control-plane nodes in values-capi-cluster.yaml",
		},
		{
			name: "returns error for existing file due to invalid yq flags in production code",
			setup: func(t *testing.T) string {
				t.Helper()
				dir := t.TempDir()
				path := filepath.Join(dir, "values.yaml")
				content := "aws:\n  controlPlane:\n    ami:\n      id: \"old-ami-111\"\n  nodeGroups:\n    - name: ng1\n      ami:\n        id: \"old-ami-222\"\n"
				require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
				return path
			},
			updates: AWSMachineTemplateUpdates{AMIID: "ami-new-999"},
			wantErr: true,
			errMsg:  "updating AMI ID for control-plane nodes in values-capi-cluster.yaml",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := tc.setup(t)

			a := &AWS{}
			err := a.UpdateCapiClusterValuesFile(context.Background(), path, tc.updates)

			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errMsg)
				return
			}
			require.NoError(t, err)
		})
	}
}
