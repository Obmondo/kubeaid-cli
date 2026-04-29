// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package kubernetes

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	veleroV1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	crFake "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
)

// newVeleroTestScheme builds a scheme with coreV1 + veleroV1 types.
func newVeleroTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	require.NoError(t, coreV1.AddToScheme(s))
	require.NoError(t, veleroV1.AddToScheme(s))
	return s
}

func TestCreateBackup(t *testing.T) {
	t.Parallel()

	scheme := newVeleroTestScheme(t)

	tests := []struct {
		name            string
		backupName      string
		interceptCreate func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.CreateOption) error
		wantErr         bool
		wantErrSubstr   string
	}{
		{
			name:       "creates a Velero Backup with the given name",
			backupName: "test-backup",
		},
		{
			name:       "creates a Velero Backup with another name",
			backupName: "pre-upgrade-backup",
		},
		{
			name:       "Create failure returns error",
			backupName: "fail-backup",
			interceptCreate: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.CreateOption) error {
				return fmt.Errorf("internal server error")
			},
			wantErr:       true,
			wantErrSubstr: "failed creating velero backup",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			builder := crFake.NewClientBuilder().
				WithScheme(scheme)

			if tc.interceptCreate != nil {
				builder = builder.WithInterceptorFuncs(interceptor.Funcs{
					Create: tc.interceptCreate,
				})
			}

			fakeClient := builder.Build()

			err := CreateBackup(context.Background(), tc.backupName, fakeClient)
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrSubstr)
				return
			}
			require.NoError(t, err)

			backup := &veleroV1.Backup{}
			err = fakeClient.Get(context.Background(),
				types.NamespacedName{Name: tc.backupName, Namespace: constants.NamespaceVelero},
				backup,
			)
			require.NoError(t, err)
			assert.Equal(t, tc.backupName, backup.Name)
			assert.Equal(t, constants.NamespaceVelero, backup.Namespace)
		})
	}
}

func TestGetLatestVeleroBackup(t *testing.T) {
	t.Parallel()

	scheme := newVeleroTestScheme(t)

	now := time.Now()
	older := now.Add(-2 * time.Hour)
	newer := now.Add(-1 * time.Hour)

	tests := []struct {
		name           string
		backups        []runtime.Object
		interceptList  func(ctx context.Context, cl client.WithWatch, list client.ObjectList, opts ...client.ListOption) error
		wantBackupName string
		wantErr        bool
		wantErrSubstr  string
	}{
		{
			name: "single backup is returned",
			backups: []runtime.Object{
				&veleroV1.Backup{
					ObjectMeta: metaV1.ObjectMeta{
						Name:      "backup-only",
						Namespace: constants.NamespaceVelero,
					},
					Status: veleroV1.BackupStatus{
						StartTimestamp: &metaV1.Time{Time: older},
					},
				},
			},
			wantBackupName: "backup-only",
		},
		{
			name: "latest backup is selected when multiple exist",
			backups: []runtime.Object{
				&veleroV1.Backup{
					ObjectMeta: metaV1.ObjectMeta{
						Name:      "backup-old",
						Namespace: constants.NamespaceVelero,
					},
					Status: veleroV1.BackupStatus{
						StartTimestamp: &metaV1.Time{Time: older},
					},
				},
				&veleroV1.Backup{
					ObjectMeta: metaV1.ObjectMeta{
						Name:      "backup-new",
						Namespace: constants.NamespaceVelero,
					},
					Status: veleroV1.BackupStatus{
						StartTimestamp: &metaV1.Time{Time: newer},
					},
				},
			},
			wantBackupName: "backup-new",
		},
		{
			name:          "no backups found returns error",
			backups:       []runtime.Object{},
			wantErr:       true,
			wantErrSubstr: "no backups found",
		},
		{
			name: "List failure returns error",
			interceptList: func(_ context.Context, _ client.WithWatch, _ client.ObjectList, _ ...client.ListOption) error {
				return fmt.Errorf("api server unavailable")
			},
			wantErr:       true,
			wantErrSubstr: "failed listing velero backups",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			builder := crFake.NewClientBuilder().
				WithScheme(scheme).
				WithStatusSubresource(&veleroV1.Backup{}).
				WithRuntimeObjects(tc.backups...)

			if tc.interceptList != nil {
				builder = builder.WithInterceptorFuncs(interceptor.Funcs{
					List: tc.interceptList,
				})
			}

			fakeClient := builder.Build()

			got, err := GetLatestVeleroBackup(context.Background(), fakeClient)
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrSubstr)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, got)
			assert.Equal(t, tc.wantBackupName, got.Name)
		})
	}
}

func TestRestoreVeleroBackup(t *testing.T) {
	t.Parallel()

	scheme := newVeleroTestScheme(t)

	tests := []struct {
		name            string
		backup          *veleroV1.Backup
		interceptCreate func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.CreateOption) error
		wantErr         bool
		wantErrSubstr   string
	}{
		{
			name: "creates a Velero Restore object for the given backup",
			backup: &veleroV1.Backup{
				ObjectMeta: metaV1.ObjectMeta{
					Name:      "my-backup",
					Namespace: constants.NamespaceVelero,
				},
			},
		},
		{
			name: "restore name matches backup name",
			backup: &veleroV1.Backup{
				ObjectMeta: metaV1.ObjectMeta{
					Name:      "another-backup",
					Namespace: constants.NamespaceVelero,
				},
			},
		},
		{
			name: "Create failure returns error",
			backup: &veleroV1.Backup{
				ObjectMeta: metaV1.ObjectMeta{
					Name:      "fail-backup",
					Namespace: constants.NamespaceVelero,
				},
			},
			interceptCreate: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.CreateOption) error {
				return fmt.Errorf("quota exceeded")
			},
			wantErr:       true,
			wantErrSubstr: "failed creating velero restore",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			builder := crFake.NewClientBuilder().
				WithScheme(scheme)

			if tc.interceptCreate != nil {
				builder = builder.WithInterceptorFuncs(interceptor.Funcs{
					Create: tc.interceptCreate,
				})
			}

			fakeClient := builder.Build()

			err := RestoreVeleroBackup(context.Background(), fakeClient, tc.backup)
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrSubstr)
				return
			}
			require.NoError(t, err)

			restore := &veleroV1.Restore{}
			err = fakeClient.Get(context.Background(),
				types.NamespacedName{Name: tc.backup.Name, Namespace: tc.backup.Namespace},
				restore,
			)
			require.NoError(t, err)
			assert.Equal(t, tc.backup.Name, restore.Name)
			assert.Equal(t, tc.backup.Name, restore.Spec.BackupName)
			require.NotNil(t, restore.Spec.RestorePVs)
			assert.True(t, *restore.Spec.RestorePVs)
		})
	}
}
