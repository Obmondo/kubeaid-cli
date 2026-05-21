// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package aws

import (
	"context"
	"fmt"
	"testing"

	argoCDV1Alpha1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	s3Types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-cli/pkg/cloud/aws/services/fake"
	"github.com/Obmondo/kubeaid-cli/pkg/config"
)

// Mutates getAccountIDFunc, syncArgoCDApp, config.ParsedGeneralConfig — sequential only.
func TestSetupDisasterRecovery(t *testing.T) {
	tests := []struct {
		name          string
		drConfig      *config.DisasterRecoveryConfig
		s3Client      *fake.S3API
		iamClient     *fake.IAMAPI
		accountIDStub func(ctx context.Context) (string, error)
		syncStub      func(ctx context.Context, name string, resources []*argoCDV1Alpha1.SyncOperationResource) error
		wantErr       bool
		errMsg        string
	}{
		{
			name:      "returns error when no disaster recovery config",
			s3Client:  &fake.S3API{CreateBucketErr: &s3Types.BucketAlreadyOwnedByYou{}},
			iamClient: &fake.IAMAPI{},
			accountIDStub: func(_ context.Context) (string, error) {
				return "123456789012", nil
			},
			syncStub: func(_ context.Context, _ string, _ []*argoCDV1Alpha1.SyncOperationResource) error {
				return nil
			},
			wantErr: true,
			errMsg:  "no disaster-recovery config provided",
		},
		{
			name: "succeeds when all calls succeed",
			drConfig: &config.DisasterRecoveryConfig{
				VeleroBackupsBucketName:        "velero-bucket",
				SealedSecretsBackupsBucketName: "ss-bucket",
			},
			s3Client:  &fake.S3API{CreateBucketErr: &s3Types.BucketAlreadyOwnedByYou{}},
			iamClient: &fake.IAMAPI{},
			accountIDStub: func(_ context.Context) (string, error) {
				return "123456789012", nil
			},
			syncStub: func(_ context.Context, _ string, _ []*argoCDV1Alpha1.SyncOperationResource) error {
				return nil
			},
		},
		{
			name: "returns error when CreateS3Bucket fails",
			drConfig: &config.DisasterRecoveryConfig{
				VeleroBackupsBucketName:        "velero-bucket",
				SealedSecretsBackupsBucketName: "ss-bucket",
			},
			s3Client:  &fake.S3API{CreateBucketErr: fmt.Errorf("access denied")},
			iamClient: &fake.IAMAPI{},
			accountIDStub: func(_ context.Context) (string, error) {
				return "123456789012", nil
			},
			syncStub: func(_ context.Context, _ string, _ []*argoCDV1Alpha1.SyncOperationResource) error {
				return nil
			},
			wantErr: true,
			errMsg:  "creating sealed-secrets backup S3 bucket",
		},
		{
			name: "returns error when GetAccountID fails",
			drConfig: &config.DisasterRecoveryConfig{
				VeleroBackupsBucketName:        "velero-bucket",
				SealedSecretsBackupsBucketName: "ss-bucket",
			},
			s3Client:  &fake.S3API{CreateBucketErr: &s3Types.BucketAlreadyOwnedByYou{}},
			iamClient: &fake.IAMAPI{},
			accountIDStub: func(_ context.Context) (string, error) {
				return "", fmt.Errorf("STS failure")
			},
			syncStub: func(_ context.Context, _ string, _ []*argoCDV1Alpha1.SyncOperationResource) error {
				return nil
			},
			wantErr: true,
			errMsg:  "getting AWS account ID for disaster recovery setup",
		},
		{
			name: "returns error when CreateIAMRoleForPolicy fails",
			drConfig: &config.DisasterRecoveryConfig{
				VeleroBackupsBucketName:        "velero-bucket",
				SealedSecretsBackupsBucketName: "ss-bucket",
			},
			s3Client:  &fake.S3API{CreateBucketErr: &s3Types.BucketAlreadyOwnedByYou{}},
			iamClient: &fake.IAMAPI{CreatePolicyErr: fmt.Errorf("policy limit exceeded")},
			accountIDStub: func(_ context.Context) (string, error) {
				return "123456789012", nil
			},
			syncStub: func(_ context.Context, _ string, _ []*argoCDV1Alpha1.SyncOperationResource) error {
				return nil
			},
			wantErr: true,
			errMsg:  "creating IAM role for sealed-secrets backuper",
		},
		{
			name: "returns error when SyncArgoCDApp fails",
			drConfig: &config.DisasterRecoveryConfig{
				VeleroBackupsBucketName:        "velero-bucket",
				SealedSecretsBackupsBucketName: "ss-bucket",
			},
			s3Client:  &fake.S3API{CreateBucketErr: &s3Types.BucketAlreadyOwnedByYou{}},
			iamClient: &fake.IAMAPI{},
			accountIDStub: func(_ context.Context) (string, error) {
				return "123456789012", nil
			},
			syncStub: func(_ context.Context, _ string, _ []*argoCDV1Alpha1.SyncOperationResource) error {
				return fmt.Errorf("sync timeout")
			},
			wantErr: true,
			errMsg:  "syncing ArgoCD app",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			savedAccountID := getAccountIDFunc
			savedSync := syncArgoCDApp
			savedCfg := config.ParsedGeneralConfig
			t.Cleanup(func() {
				getAccountIDFunc = savedAccountID
				syncArgoCDApp = savedSync
				config.ParsedGeneralConfig = savedCfg
			})

			config.ParsedGeneralConfig = &config.GeneralConfig{
				Cluster: config.ClusterConfig{Name: "test-cluster"},
				Cloud: config.CloudConfig{
					AWS:              &config.AWSConfig{Region: "us-east-1"},
					DisasterRecovery: tc.drConfig,
				},
			}
			getAccountIDFunc = tc.accountIDStub
			syncArgoCDApp = tc.syncStub

			a := &AWS{
				s3Client:  tc.s3Client,
				iamClient: tc.iamClient,
			}

			err := a.SetupDisasterRecovery(context.Background())
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errMsg)
				return
			}
			require.NoError(t, err)
		})
	}
}
