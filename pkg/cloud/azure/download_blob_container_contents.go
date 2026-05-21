// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package azure

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"

	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/utils"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/logger"
)

type blobDownloader interface {
	DownloadStream(ctx context.Context, containerName string, blobName string, o *azblob.DownloadStreamOptions) (azblob.DownloadStreamResponse, error)
}

var listBlobNamesFn = func(ctx context.Context, blobClient *azblob.Client, containerName string) ([]string, error) {
	pager := blobClient.NewListBlobsFlatPager(containerName,
		&container.ListBlobsFlatOptions{
			Include: container.ListBlobsInclude{
				Snapshots: true,
				Versions:  false,
			},
		},
	)

	var names []string
	for pager.More() {
		response, err := pager.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, blob := range response.Segment.BlobItems {
			names = append(names, *blob.Name)
		}
	}
	return names, nil
}

var getDownloadedStorageBucketContentsDir = utils.GetDownloadedStorageBucketContentsDir

// DownloadBlobContainerContents downloads contents of the given Azure Blob Container locally.
// If the contents are gZip encoded, then you can choose to gZip decode them after download.
// The download path is decided by utils.GetDownloadedStorageBucketContentsDir.
func DownloadBlobContainerContents(ctx context.Context,
	blobClient *azblob.Client,
	blobContainerName string,
) error {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("blob-container-name", blobContainerName),
	})

	downloadDir := getDownloadedStorageBucketContentsDir(blobContainerName)
	err := os.MkdirAll(downloadDir, 0o750)
	if err != nil {
		return fmt.Errorf("creating directory %s: %w", downloadDir, err)
	}

	blobNames, err := listBlobNamesFn(ctx, blobClient, blobContainerName)
	if err != nil {
		return fmt.Errorf("listing blobs: %w", err)
	}

	for _, name := range blobNames {
		if err := downloadBlobContent(ctx, &downloadBlobContentArgs{
			BlobClient: blobClient,

			DownloadDir: downloadDir,

			BlobContainerName: blobContainerName,
			BlobName:          name,
		}); err != nil {
			return fmt.Errorf("downloading blob %s: %w", name, err)
		}
	}

	return nil
}

type downloadBlobContentArgs struct {
	BlobClient blobDownloader

	DownloadDir,

	BlobContainerName,
	BlobName string
}

func downloadBlobContent(ctx context.Context, args *downloadBlobContentArgs) error {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("blob", args.BlobName),
	})

	filePath := filepath.Join(args.DownloadDir, args.BlobName)

	// Guard against path traversal: ensure the resolved path stays under the download directory.
	if !strings.HasPrefix(filepath.Clean(filePath), filepath.Clean(args.DownloadDir)+string(os.PathSeparator)) {
		return fmt.Errorf("blob name %q escapes download directory", args.BlobName)
	}

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
		err := utils.CreateIntermediateDirsForFile(filePath)
		if err != nil {
			return fmt.Errorf("creating intermediate dirs for %s: %w", filePath, err)
		}
	}

	// Create the file where the contents of the given blob will be stored.
	destinationFile, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("opening file %s: %w", filePath, err)
	}
	defer destinationFile.Close()

	// Fetch the blob.
	response, err := args.BlobClient.DownloadStream(ctx, args.BlobContainerName, args.BlobName, nil)
	if err != nil {
		return fmt.Errorf("downloading blob content: %w", err)
	}
	defer response.Body.Close()

	blobContentReader := response.Body

	// gzip decode blob content (if required).
	if isFileGzipped {
		gzipReader, err := gzip.NewReader(response.Body)
		if err != nil {
			return fmt.Errorf("creating a gZip reader: %w", err)
		}
		defer gzipReader.Close()

		blobContentReader = gzipReader
	}

	// Copy contents of the fetched blob to the file.
	_, err = io.Copy(destinationFile, blobContentReader)
	if err != nil {
		return fmt.Errorf("writing blob content to file: %w", err)
	}

	return nil
}
