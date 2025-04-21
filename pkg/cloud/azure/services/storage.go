package services

import (
	"context"
	"log/slog"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/storage/armstorage"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
)

type CreateStorageAccountArgs struct {
	StorageAccountsClient *armstorage.AccountsClient
	ResourceGroupName,
	Name string // The Storage Account name must between 3-24 characters and unqiue within Azure.
}

/*
Creates an appropriate Azure Storage Account, if one doesn't already exist.

	An Azure storage account contains all of your Azure Storage data objects: blobs, files, queues,
	and tables. The storage account provides a unique namespace for your Azure Storage data.

	REFERENCE : https://learn.microsoft.com/en-us/azure/storage/common/storage-account-overview.
*/
func CreateStorageAccount(ctx context.Context, args *CreateStorageAccountArgs) {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("storage-account-name", args.Name),
	})

	slog.InfoContext(ctx, "Creating / updating Azure Storage Account")

	responsePoller, err := args.StorageAccountsClient.BeginCreate(ctx,
		args.ResourceGroupName,
		args.Name,
		armstorage.AccountCreateParameters{
			// Standard Storage Account type, recommended for most of the scenarios.
			Kind: to.Ptr(armstorage.KindStorageV2),

			SKU: &armstorage.SKU{
				// You can view all the Storage Account SKU types here :
				// https://learn.microsoft.com/en-us/rest/api/storagerp/srp_sku_types
				Name: to.Ptr(armstorage.SKUNameStandardLRS), // Standard Locally Redundant Storage.
			},

			Location: &config.ParsedGeneralConfig.Cloud.Azure.Location,

			Properties: &armstorage.AccountPropertiesCreateParameters{
				// Since very frequent resources will be coming from all the Kubernetes workloads, we
				// will be using the Hot tier.
				// It has the highest storage costs, but the lowest access costs.
				// REFERENCE : https://learn.microsoft.com/en-us/azure/storage/blobs/access-tiers-overview.
				AccessTier: to.Ptr(armstorage.AccessTierHot),

				AllowBlobPublicAccess: to.Ptr(true),

				// We don't want any encryption at rest, since there'll be only 2 documents and both of
				// them will be public.
			},

			Tags: map[string]*string{
				"cluster": &config.ParsedGeneralConfig.Cluster.Name,
			},
		},
		nil,
	)
	if err != nil {
		// Skip, if the Storage Account already exists.
		responseError, ok := err.(*azcore.ResponseError)
		if ok && responseError.StatusCode == constants.AzureResponseStatusCodeResourceAlreadyExists {
			slog.InfoContext(ctx, "Azure Storage Account already exists")
			return
		}

		assert.AssertErrNil(ctx, err, "Failed creating / updating Azure Storage Account")
	}

	_, err = responsePoller.PollUntilDone(ctx, nil)
	assert.AssertErrNil(ctx, err, "Failed creating / updating Azure Storage Account")

	slog.InfoContext(ctx, "Created / updated Azure Storage Account")
}

type CreateBlobContainerArgs struct {
	BlobContainersClient *armstorage.BlobContainersClient
	ResourceGroupName,
	StorageAccountName,
	BlobContainerName string
}

/*
Creates a Blob Container in the given Storage Account, if one doesn't already exist.

	Azure Blob Storage is Microsoft's object storage solution for the cloud.
	A container organizes a set of blobs, similar to a directory in a file system.
	A storage account can include an unlimited number of containers, and a container can store an
	unlimited number of blobs.

	REFERENCE : https://learn.microsoft.com/en-us/azure/storage/blobs/storage-blobs-introduction#containers
*/
func CreateBlobContainer(ctx context.Context, args *CreateBlobContainerArgs) {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("blob-container-name", args.BlobContainerName),
	})

	_, err := args.BlobContainersClient.Create(ctx,
		args.ResourceGroupName,
		args.StorageAccountName,
		args.BlobContainerName,
		armstorage.BlobContainer{
			ContainerProperties: &armstorage.ContainerProperties{
				PublicAccess: to.Ptr(armstorage.PublicAccessBlob),
			},
		},
		nil,
	)
	if err != nil {
		// Skip, if the Storage Account already exists.
		responseError, ok := err.(*azcore.ResponseError)
		if ok && responseError.StatusCode == constants.AzureResponseStatusCodeResourceAlreadyExists {
			slog.InfoContext(ctx, "Azure Blob Container already exists")
			return
		}

		assert.AssertErrNil(ctx, err, "Failed creating Azure Blob Container")
	}

	slog.InfoContext(ctx, "Created Azure Blob Container")
}
