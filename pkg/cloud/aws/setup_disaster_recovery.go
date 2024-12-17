package aws

import (
	"context"
	"fmt"

	"github.com/Obmondo/kubeaid-bootstrap-script/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud/aws/services"
	"github.com/Obmondo/kubeaid-bootstrap-script/utils"
	argoCDV1Alpha1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	"github.com/sagikazarmark/slog-shim"
)

// Sets up the provisioned cluster for Disaster Recovery.
// NOTE : Picks up AWS credentials from the environment.
func (a *AWS) SetupDisasterRecovery(ctx context.Context) {
	if config.ParsedConfig.Cloud.AWS.DisasterRecovery == nil {
		return
	}

	slog.InfoContext(ctx, "Setting up Disaster Recovery")

	// Create S3 bucket where Sealed Secrets will be backed up.
	sealedSecretBackupsS3BucketName := config.ParsedConfig.Cloud.AWS.DisasterRecovery.SealedSecretsBackupS3BucketName
	services.CreateS3Bucket(ctx, a.s3Client, sealedSecretBackupsS3BucketName)
	//
	// Create S3 bucket where Kubernetes Objects will be backed up (by Velero).
	veleroBackupsS3BucketName := config.ParsedConfig.Cloud.AWS.DisasterRecovery.VeleroBackupsS3BucketName
	services.CreateS3Bucket(ctx, a.s3Client, veleroBackupsS3BucketName)

	clusterName := config.ParsedConfig.Cluster.Name

	// Create IAM Policy for Sealed Secrets Backuper.
	sealedSecretsBackuperIAMPolicyName := fmt.Sprintf("sealed-secrets-backuper-%s", clusterName)
	services.CreateIAMPolicy(ctx, a.iamClient, sealedSecretsBackuperIAMPolicyName, getSealedSecretsBackuperIAMPolicy())
	//
	// Create IAM Policy for Velero.
	veleroIAMPolicyName := fmt.Sprintf("velero-%s", clusterName)
	services.CreateIAMPolicy(ctx, a.iamClient, veleroIAMPolicyName, getVeleroIAMPolicy())

	// Sync Kube2IAM, K8sConfigs, Velero and SealedSecrets ArgoCD Apps.
	argocdAppsToBeSynced := []string{
		"kube2iam",
		"k8s-configs",
		"velero",
		"sealed-secrets",
	}
	for _, argoCDApp := range argocdAppsToBeSynced {
		utils.SyncArgoCDApp(ctx, argoCDApp, []*argoCDV1Alpha1.SyncOperationResource{})
	}
}
