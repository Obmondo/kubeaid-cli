// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package services

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3Types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/sagikazarmark/slog-shim"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/utils"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/logger"
)

type S3API interface {
	CreateBucket(ctx context.Context, params *s3.CreateBucketInput, optFns ...func(*s3.Options)) (*s3.CreateBucketOutput, error)
	HeadBucket(ctx context.Context, params *s3.HeadBucketInput, optFns ...func(*s3.Options)) (*s3.HeadBucketOutput, error)
	ListObjectsV2(ctx context.Context, params *s3.ListObjectsV2Input, optFns ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
	GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error)
}

var getDownloadedStorageBucketContentsDir = utils.GetDownloadedStorageBucketContentsDir

// Creates S3 Bucket.
func CreateS3Bucket(ctx context.Context, s3Client S3API, name string) error {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("s3Bucket", name),
	})

	createBucketInput := &s3.CreateBucketInput{
		Bucket: aws.String(name),
	}
	if config.ParsedGeneralConfig.Cloud.AWS.Region != "us-east-1" {
		createBucketInput.CreateBucketConfiguration = &s3Types.CreateBucketConfiguration{
			LocationConstraint: s3Types.BucketLocationConstraint(
				config.ParsedGeneralConfig.Cloud.AWS.Region,
			),
		}
	}
	_, err := s3Client.CreateBucket(ctx, createBucketInput)

	var alreadyOwned *s3Types.BucketAlreadyOwnedByYou
	switch {
	case err == nil:
		if err := s3.NewBucketExistsWaiter(s3Client).
			Wait(ctx, &s3.HeadBucketInput{Bucket: aws.String(name)}, time.Minute); err != nil {
			return fmt.Errorf("waiting for S3 bucket %s to be created: %w", name, err)
		}
		slog.InfoContext(ctx, "Created S3 bucket")

	case errors.As(err, &alreadyOwned):
		slog.WarnContext(ctx, "S3 bucket already exists and is owned by you")

	default:
		return fmt.Errorf("creating S3 bucket %s: %w", name, err)
	}

	return nil
}

// Downloads the contents of the given S3 bucket locally.
// If the contents are gZip encoded, then you can choose to gZip decode them after download.
// NOTE : The download path is decided by utils.GetDownloadedStorageBucketContentsDir( ).
func DownloadS3BucketContents(ctx context.Context,
	s3Client S3API,
	bucketName string,
	gzipDecode bool,
) error {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("s3-bucket", bucketName),
	})

	slog.InfoContext(ctx, "Downloading contents of S3 bucket")

	downloadDir := getDownloadedStorageBucketContentsDir(bucketName)
	if err := os.MkdirAll(downloadDir, 0o750); err != nil {
		return fmt.Errorf("creating directory %s: %w", downloadDir, err)
	}

	listObjectsInput := s3.ListObjectsV2Input{
		Bucket: aws.String(bucketName),
	}
	for {
		listObjectsOutput, err := s3Client.ListObjectsV2(ctx, &listObjectsInput)
		if err != nil {
			return fmt.Errorf("listing objects in S3 bucket %s: %w", bucketName, err)
		}

		for _, object := range listObjectsOutput.Contents {
			if err := downloadS3Object(ctx, s3Client, &bucketName, &downloadDir, object.Key, gzipDecode); err != nil {
				return fmt.Errorf("downloading S3 object %s: %w", *object.Key, err)
			}
		}

		if !*listObjectsOutput.IsTruncated {
			break
		}
		listObjectsInput.ContinuationToken = listObjectsOutput.ContinuationToken
	}

	return nil
}

func downloadS3Object(ctx context.Context,
	s3Client S3API,
	bucketName, downloadDir, objectKey *string,
	gzipDecode bool,
) error {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("object", *objectKey),
	})

	filePath := filepath.Join(*downloadDir, *objectKey)

	// Guard against path traversal: ensure the resolved path stays under the download directory.
	if !strings.HasPrefix(filepath.Clean(filePath), filepath.Clean(*downloadDir)+string(os.PathSeparator)) {
		return fmt.Errorf("object key %q escapes download directory", *objectKey)
	}

	if strings.Contains(*objectKey, "/") {
		if err := utils.CreateIntermediateDirsForFile(filePath); err != nil {
			return fmt.Errorf("creating intermediate dirs for %s: %w", filePath, err)
		}
	}

	destinationFile, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("opening file %s: %w", filePath, err)
	}
	defer destinationFile.Close()

	getObjectOutput, err := s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: bucketName,
		Key:    objectKey,
	})
	if err != nil {
		return fmt.Errorf("getting S3 object: %w", err)
	}
	defer getObjectOutput.Body.Close()

	s3ObjectContentReader := getObjectOutput.Body

	if gzipDecode {
		gzipReader, err := gzip.NewReader(getObjectOutput.Body)
		if err != nil {
			return fmt.Errorf("creating gZip reader: %w", err)
		}
		defer gzipReader.Close()

		s3ObjectContentReader = gzipReader
	}

	if _, err = io.Copy(destinationFile, s3ObjectContentReader); err != nil {
		return fmt.Errorf("writing S3 object contents to file: %w", err)
	}

	return nil
}
