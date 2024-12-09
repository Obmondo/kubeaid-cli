package aws

import (
	"context"
	"log/slog"
	"os"

	"github.com/Obmondo/kubeaid-bootstrap-script/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/utils/assert"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func (a *AWS) GetLatestBackupName(ctx context.Context) string {
	listObjectsOutput, err := a.s3Client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: &config.ParsedConfig.Cloud.AWS.DisasterRecovery.VeleroBackupsS3BucketName,
		Prefix: aws.String("backups/"),
	})
	assert.AssertErrNil(ctx, err, "Failed to list S3 objects with prefix 'backups/'")

	if len(listObjectsOutput.Contents) == 0 {
		slog.ErrorContext(ctx, "No Velero backups found")
		os.Exit(1)
	}

	panic("unimplemented")
}
