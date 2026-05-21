// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package aws

import (
	"context"
	"fmt"

	argoCDV1Alpha1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	"github.com/sagikazarmark/slog-shim"

	"github.com/Obmondo/kubeaid-cli/pkg/cloud/aws/services"
	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/kubernetes"
)

var getAccountIDFunc = GetAccountID

var syncArgoCDApp = kubernetes.SyncArgoCDApp

// SetupDisasterRecovery sets up the provisioned cluster for Disaster Recovery.
// NOTE: Picks up AWS credentials from the environment.
func (a *AWS) SetupDisasterRecovery(ctx context.Context) error {
	disasterRecoveryConfig := config.ParsedGeneralConfig.Cloud.DisasterRecovery
	if disasterRecoveryConfig == nil {
		return fmt.Errorf("no disaster-recovery config provided")
	}

	slog.InfoContext(ctx, "Setting up Disaster Recovery")

	if err := services.CreateS3Bucket(ctx, a.s3Client, disasterRecoveryConfig.SealedSecretsBackupsBucketName); err != nil {
		return fmt.Errorf("creating sealed-secrets backup S3 bucket: %w", err)
	}

	if err := services.CreateS3Bucket(ctx, a.s3Client, disasterRecoveryConfig.VeleroBackupsBucketName); err != nil {
		return fmt.Errorf("creating Velero backup S3 bucket: %w", err)
	}

	clusterName := config.ParsedGeneralConfig.Cluster.Name

	accountID, err := getAccountIDFunc(ctx)
	if err != nil {
		return fmt.Errorf("getting AWS account ID for disaster recovery setup: %w", err)
	}

	sealedSecretsBackuperIAMPolicyName := fmt.Sprintf("sealed-secrets-backuper-%s", clusterName)
	if err := services.CreateIAMRoleForPolicy(ctx,
		accountID,
		a.iamClient,
		sealedSecretsBackuperIAMPolicyName,
		getSealedSecretsBackuperIAMPolicy(),
		getIAMTrustPolicy(accountID),
	); err != nil {
		return fmt.Errorf("creating IAM role for sealed-secrets backuper: %w", err)
	}

	veleroIAMPolicyName := fmt.Sprintf("velero-%s", clusterName)
	if err := services.CreateIAMRoleForPolicy(ctx,
		accountID,
		a.iamClient,
		veleroIAMPolicyName,
		getVeleroIAMPolicy(),
		getIAMTrustPolicy(accountID),
	); err != nil {
		return fmt.Errorf("creating IAM role for velero: %w", err)
	}

	argocdAppsToBeSynced := []string{
		"kube2iam",
		"k8s-configs",
		constants.ArgoCDAppVelero,
		"sealed-secrets",
	}
	for _, argoCDApp := range argocdAppsToBeSynced {
		if err := syncArgoCDApp(ctx, argoCDApp, []*argoCDV1Alpha1.SyncOperationResource{}); err != nil {
			return fmt.Errorf("syncing ArgoCD app %s: %w", argoCDApp, err)
		}
	}

	return nil
}
