package services

import (
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Obmondo/kubeaid-bootstrap-script/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/utils"
	"github.com/Obmondo/kubeaid-bootstrap-script/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/utils/logger"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3Types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/sagikazarmark/slog-shim"
)

// Creates S3 Bucket.
func CreateS3Bucket(ctx context.Context, s3Client *s3.Client, name string) {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("s3Bucket", name),
	})

	createBucketInput := &s3.CreateBucketInput{
		Bucket: aws.String(name),
	}
	if config.ParsedConfig.Cloud.AWS.Credentials.AWSRegion != "us-east-1" {
		createBucketInput.CreateBucketConfiguration = &s3Types.CreateBucketConfiguration{
			LocationConstraint: s3Types.BucketLocationConstraint(config.ParsedConfig.Cloud.AWS.Credentials.AWSRegion),
		}
	}
	_, err := s3Client.CreateBucket(ctx, createBucketInput)
	switch err.(type) {
	// S3 Bucket already exists and is owned by the user.
	case *s3Types.BucketAlreadyOwnedByYou:
		slog.WarnContext(ctx, "S3 bucket already exists and is owned by you")

	default:
		assert.AssertErrNil(ctx, err, "Failed creating S3 bucket")

		// Wait fo the S3 bucket to be created.
		err = s3.NewBucketExistsWaiter(s3Client).
			Wait(ctx, &s3.HeadBucketInput{Bucket: aws.String(name)}, time.Minute)
		assert.AssertErrNil(ctx, err, "Failed waiting for S3 bucket to be created")
		slog.InfoContext(ctx, "Created S3 bucket")
	}
}

// Downloads the contents of the given S3 bucket locally.
// If the contents are gZip encoded, then you can choose to gZip decode them after download.
// NOTE : The download path is decided by utils.GetDownloadedStorageBucketContentsDir( ).
func DownloadS3BucketContents(ctx context.Context,
	s3Client *s3.Client,
	bucketName string,
	gzipDecode bool,
) {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("s3-bucket", bucketName),
	})

	slog.InfoContext(ctx, "Downloading contents of S3 bucket")

	// Create directory where S3 objects will be downloaded.
	downloadDir := utils.GetDownloadedStorageBucketContentsDir(bucketName)
	err := os.MkdirAll(downloadDir, os.ModePerm)
	assert.AssertErrNil(ctx, err, "Failed creating directory", slog.String("path", downloadDir))

	listObjectsInput := s3.ListObjectsV2Input{
		Bucket: aws.String(bucketName),
	}
	for {
		listObjectsOutput, err := s3Client.ListObjectsV2(ctx, &listObjectsInput)
		assert.AssertErrNil(ctx, err, "Failed listing objects in S3 bucket")

		// Iterate through the S3 objects and download content of each object.
		for _, object := range listObjectsOutput.Contents {
			downloadS3Object(ctx, s3Client, &bucketName, &downloadDir, object.Key, gzipDecode)
		}

		if !*listObjectsOutput.IsTruncated {
			break
		}
		listObjectsInput.ContinuationToken = listObjectsOutput.ContinuationToken
	}
}

// Downloads the content of the given S3 object locally.
func downloadS3Object(ctx context.Context, s3Client *s3.Client, bucketName, downloadDir, objectKey *string, gzipDecode bool) {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("object", *objectKey),
	})

	filePath := filepath.Join(*downloadDir, *objectKey)

	// Create intermediate directories (if required).
	if strings.Contains(*objectKey, "/") {
		err := os.MkdirAll(filePath, os.ModePerm)
		assert.AssertErrNil(ctx, err, "Failed creating directory", slog.String("path", *downloadDir))
	}

	// Create the file where the contents of the given S3 object will be stored.
	destinationFile, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	assert.AssertErrNil(ctx, err, "Failed opening file", slog.String("path", filePath))
	defer destinationFile.Close()

	// Fetch the S3 object.
	getObjectOutput, err := s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: bucketName,
		Key:    objectKey,
	})
	assert.AssertErrNil(ctx, err, "Failed getting S3 Object")
	defer getObjectOutput.Body.Close()

	s3ObjectContentReader := getObjectOutput.Body

	// gZip decode S3 object content (if required).
	if gzipDecode {
		gzipReader, err := gzip.NewReader(getObjectOutput.Body)
		assert.AssertErrNil(ctx, err, "Failed creating a gZip reader")
		defer gzipReader.Close()

		s3ObjectContentReader = gzipReader
	}

	// Copy contents of the fetched S3 object to the file.
	_, err = io.Copy(destinationFile, s3ObjectContentReader)
	assert.AssertErrNil(ctx, err, "Failed writing S3 Object contents to file")
}
