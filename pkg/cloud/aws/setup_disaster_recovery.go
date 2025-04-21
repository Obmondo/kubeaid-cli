package aws

import (
	"context"
	"fmt"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud/aws/services"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/kubernetes"
	argoCDV1Alpha1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	"github.com/sagikazarmark/slog-shim"
)

// Sets up the provisioned cluster for Disaster Recovery.
// NOTE : Picks up AWS credentials from the environment.
func (a *AWS) SetupDisasterRecovery(ctx context.Context) {
	awsConfig := config.ParsedGeneralConfig.Cloud.AWS
	assert.AssertNotNil(ctx, awsConfig.DisasterRecovery, "No AWS disaster-recovery config provided")

	slog.InfoContext(ctx, "Setting up Disaster Recovery")

	// Create S3 bucket where Sealed Secrets will be backed up.
	sealedSecretBackupsBucketName := awsConfig.DisasterRecovery.SealedSecretsBackupBucketName
	services.CreateS3Bucket(ctx, a.s3Client, sealedSecretBackupsBucketName)
	//
	// Create S3 bucket where Kubernetes Objects will be backed up (by Velero).
	veleroBackupsBucketName := awsConfig.DisasterRecovery.VeleroBackupsBucketName
	services.CreateS3Bucket(ctx, a.s3Client, veleroBackupsBucketName)

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
		"velero",
		"sealed-secrets",
	}
	for _, argoCDApp := range argocdAppsToBeSynced {
		kubernetes.SyncArgoCDApp(ctx, argoCDApp, []*argoCDV1Alpha1.SyncOperationResource{})
	}
}
