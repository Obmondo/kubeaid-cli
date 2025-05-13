package services

import (
	"compress/gzip"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/storage/armstorage"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils"
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
		//nolint:errorlint
		responseError, ok := err.(*azcore.ResponseError)
		if ok &&
			responseError.StatusCode == constants.AzureResponseStatusCodeResourceAlreadyExists {
			slog.InfoContext(ctx, "Azure Storage Account already exists")
			return
		}

		assert.AssertErrNil(ctx, err, "Failed creating / updating Azure Storage Account")
	}

	_, err = responsePoller.PollUntilDone(ctx, nil)
	assert.AssertErrNil(ctx, err, "Failed creating / updating Azure Storage Account")

	// Save the Azure Storage Account's access key as a global variable.
	// We need to pass it to the templates later.

	response, err := args.StorageAccountsClient.ListKeys(ctx,
		args.ResourceGroupName,
		args.Name,
		nil,
	)
	assert.AssertErrNil(ctx, err, "Failed listing Azure Storage Account keys")

	globals.AzureStorageAccountAccessKey = *(response.Keys[0].Value)
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

	slog.InfoContext(ctx, "Creating Azure Blob Container")

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
		// Skip, if the Azure Blob Container already exists.
		//nolint:errorlint
		responseError, ok := err.(*azcore.ResponseError)
		if ok &&
			responseError.StatusCode == constants.AzureResponseStatusCodeResourceAlreadyExists {
			slog.InfoContext(ctx, "Azure Blob Container already exists")
			return
		}

		assert.AssertErrNil(ctx, err, "Failed creating Azure Blob Container")
	}
}

// Downloads contents of the given Azure Blob Container locally.
// If the contents are gZip encoded, then you can choose to gZip decode them after download.
// NOTE : The download path is decided by utils.GetDownloadedStorageBucketContentsDir( ).
func DownloadBlobContainerContents(ctx context.Context,
	blobClient *azblob.Client,
	blobContainerName string,
) {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("blob-container-name", blobContainerName),
	})

	// Create directory where S3 objects will be downloaded.
	downloadDir := utils.GetDownloadedStorageBucketContentsDir(blobContainerName)
	err := os.MkdirAll(downloadDir, os.ModePerm)
	assert.AssertErrNil(ctx, err, "Failed creating directory", slog.String("path", downloadDir))

	blobsPager := blobClient.NewListBlobsFlatPager(blobContainerName,
		&container.ListBlobsFlatOptions{
			Include: container.ListBlobsInclude{
				Snapshots: true,
				Versions:  false,
			},
		},
	)
	for blobsPager.More() {
		response, err := blobsPager.NextPage(ctx)
		assert.AssertErrNil(ctx, err, "Failed listing blobs")

		// Iterate through the blobs and download content of each blob.
		for _, blob := range response.Segment.BlobItems {
			downloadBlobContent(ctx, &DownloadBlobContentArgs{
				BlobClient: blobClient,

				DownloadDir: downloadDir,

				BlobContainerName: blobContainerName,
				BlobName:          *blob.Name,
			})
		}
	}
}

type DownloadBlobContentArgs struct {
	BlobClient *azblob.Client

	DownloadDir,

	BlobContainerName,
	BlobName string
}

func downloadBlobContent(ctx context.Context, args *DownloadBlobContentArgs) {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("blob", args.BlobName),
	})

	isGzipped := false
	filePath := filepath.Join(args.DownloadDir, args.BlobName)
	//
	// If it's a gzipped file.
	if strings.HasSuffix(args.BlobName, constants.GzippedFilenameSuffix) {
		isGzipped = true
		filePath, _ = strings.CutSuffix(filePath, constants.GzippedFilenameSuffix)
	}

	// Create intermediate directories (if required).
	if strings.Contains(filePath, "/") {
		utils.CreateIntermediateDirsForFile(ctx, filePath)
	}

	// Create the file where the contents of the given blob will be stored.
	destinationFile, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.ModePerm)
	assert.AssertErrNil(ctx, err, "Failed opening file", slog.String("path", filePath))
	defer destinationFile.Close()

	// Fetch the blob.
	response, err := args.BlobClient.DownloadStream(ctx, args.BlobContainerName, args.BlobName, nil)
	assert.AssertErrNil(ctx, err, "Failed downloading blob content")
	defer response.Body.Close()

	blobContentReader := response.Body

	// gzip decode blob content (if required).
	if isGzipped {
		gzipReader, err := gzip.NewReader(response.Body)
		assert.AssertErrNil(ctx, err, "Failed creating a gZip reader")
		defer gzipReader.Close()

		blobContentReader = gzipReader
	}

	// Copy contents of the fetched blob to the file.
	_, err = io.Copy(destinationFile, blobContentReader)
	assert.AssertErrNil(ctx, err, "Failed writing blob content to file")
}
