package azure

import (
	"context"
	"log/slog"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud/azure/services"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/kubernetes"
	argoCDV1Alpha1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
)

// Sets up the provisioned cluster for Disaster Recovery.
func (a *Azure) SetupDisasterRecovery(ctx context.Context) {
	azureConfig := config.ParsedGeneralConfig.Cloud.Azure
	assert.AssertNotNil(ctx, azureConfig.DisasterRecovery, "No Azure disaster-recovery config provided")

	slog.InfoContext(ctx, "Setting up Disaster Recovery")

	// Create Blob Container where Velero backups will be stored.
	services.CreateBlobContainer(ctx, &services.CreateBlobContainerArgs{
		ResourceGroupName:    a.resourceGroupName,
		StorageAccountName:   azureConfig.StorageAccount,
		BlobContainersClient: a.storageClientFactory.NewBlobContainersClient(),
		BlobContainerName:    azureConfig.DisasterRecovery.VeleroBackupsBucketName,
	})

	// Create Blob Container where Sealed Secrets private keys will be backed up.
	services.CreateBlobContainer(ctx, &services.CreateBlobContainerArgs{
		ResourceGroupName:    a.resourceGroupName,
		StorageAccountName:   azureConfig.StorageAccount,
		BlobContainersClient: a.storageClientFactory.NewBlobContainersClient(),
		BlobContainerName:    azureConfig.DisasterRecovery.SealedSecretsBackupBucketName,
	})

	// Sync Velero ArgoCD App.
	kubernetes.SyncArgoCDApp(ctx, constants.ArgoCDAppVelero, []*argoCDV1Alpha1.SyncOperationResource{})
}
