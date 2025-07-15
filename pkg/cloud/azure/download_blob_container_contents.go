package azure

import (
	"compress/gzip"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
)

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

	filePath := filepath.Join(args.DownloadDir, args.BlobName)
	isFileGzipped := false
	//
	// If it's a gzipped file.
	if strings.HasSuffix(filePath, constants.GzippedFilenameSuffix) {
		isFileGzipped = true
		filePath, _ = strings.CutSuffix(filePath, constants.GzippedFilenameSuffix)
	}
	//
	// Also, if the file name doesn't have any extension (for e.g., in case of files created by the
	// Sealed Secrets Backuper CRON), we'll use the yaml file extension.
	if len(filepath.Ext(filePath)) == 0 {
		filePath += ".yaml"
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
	if isFileGzipped {
		gzipReader, err := gzip.NewReader(response.Body)
		assert.AssertErrNil(ctx, err, "Failed creating a gZip reader")
		defer gzipReader.Close()

		blobContentReader = gzipReader
	}

	// Copy contents of the fetched blob to the file.
	_, err = io.Copy(destinationFile, blobContentReader)
	assert.AssertErrNil(ctx, err, "Failed writing blob content to file")
}
