// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package aws

import (
	"context"
	"fmt"

	argoCDV1Alpha1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	"github.com/sagikazarmark/slog-shim"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud/aws/services"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/kubernetes"
)

// Sets up the provisioned cluster for Disaster Recovery.
// NOTE : Picks up AWS credentials from the environment.
func (a *AWS) SetupDisasterRecovery(ctx context.Context) {
	disasterRecoveryConfig := config.ParsedGeneralConfig.Cloud.DisasterRecovery
	assert.AssertNotNil(ctx, disasterRecoveryConfig, "No disaster-recovery config provided")

	slog.InfoContext(ctx, "Setting up Disaster Recovery")

	// Create S3 bucket where Sealed Secrets will be backed up.
	services.CreateS3Bucket(ctx, a.s3Client, disasterRecoveryConfig.SealedSecretsBackupsBucketName)
	//
	// Create S3 bucket where Kubernetes Objects will be backed up (by Velero).
	services.CreateS3Bucket(ctx, a.s3Client, disasterRecoveryConfig.VeleroBackupsBucketName)

	var (
		clusterName = config.ParsedGeneralConfig.Cluster.Name
		accountID   = GetAccountID(ctx)
	)

	// Create IAM Policy for Sealed Secrets Backuper.
	sealedSecretsBackuperIAMPolicyName := fmt.Sprintf("sealed-secrets-backuper-%s", clusterName)
	services.CreateIAMRoleForPolicy(ctx,
		accountID,
		a.iamClient,
		sealedSecretsBackuperIAMPolicyName,
		getSealedSecretsBackuperIAMPolicy(),
		getIAMTrustPolicy(ctx),
	)

	// Create IAM Policy for Velero.
	veleroIAMPolicyName := fmt.Sprintf("velero-%s", clusterName)
	services.CreateIAMRoleForPolicy(ctx,
		accountID,
		a.iamClient,
		veleroIAMPolicyName,
		getVeleroIAMPolicy(),
		getIAMTrustPolicy(ctx),
	)

	// Sync Kube2IAM, K8sConfigs, Velero and SealedSecrets ArgoCD Apps.
	argocdAppsToBeSynced := []string{
		"kube2iam",
		"k8s-configs",
		constants.ArgoCDAppVelero,
		"sealed-secrets",
	}
	for _, argoCDApp := range argocdAppsToBeSynced {
		kubernetes.SyncArgoCDApp(ctx, argoCDApp, []*argoCDV1Alpha1.SyncOperationResource{})
	}
}
