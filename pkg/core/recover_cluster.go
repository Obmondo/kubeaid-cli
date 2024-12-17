package core

import (
	"context"

	"github.com/Obmondo/kubeaid-bootstrap-script/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud/aws/services"
	"github.com/Obmondo/kubeaid-bootstrap-script/utils/assert"
	awsSDKGoV2Config "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func RecoverCluster(ctx context.Context, cloudProvider cloud.CloudProvider) {
	switch {
	case config.ParsedConfig.Cloud.Hetzner != nil:
		assert.AssertNil(ctx, config.ParsedConfig.Cloud.Hetzner, "Disaster recovery isn't supported for Hetzner")

	case config.ParsedConfig.Cloud.AWS != nil:
		assert.AssertNotNil(ctx, config.ParsedConfig.Cloud.AWS.DisasterRecovery, "disasterRecovery section in the config file, can't be empty")
	}

	// Load AWS SDK config.
	awsSDKConfig, err := awsSDKGoV2Config.LoadDefaultConfig(ctx)
	assert.AssertErrNil(ctx, err, "Failed initiating AWS SDK config")

	s3Client := s3.NewFromConfig(awsSDKConfig)

	/*
		Pull and gzip decode backed up (by Sealed Secrets backuper CRONJob) Kubernetes Secrets from S3
		bucket. Each Kubernetes Secret contains a Sealed Secrets encryption key.

		The script uresponsible for this backup process can be found here :
		https://github.com/Obmondo/kubeaid/blob/master/argocd-helm-charts/sealed-secrets/templates/configmap.yaml

		And you can read about Sealed Secrets key rotation from these references :
			(1) https://playbook.stakater.com/content/workshop/sealed-secrets/management.html.
			(2) https://github.com/bitnami-labs/sealed-secrets?tab=readme-ov-file#secret-rotation
	*/
	services.DownloadS3BucketContents(ctx, s3Client, config.ParsedConfig.Cloud.AWS.DisasterRecovery.SealedSecretsBackupS3BucketName, true)

	// Bootstrap the new cluster.
	BootstrapCluster(ctx, true, false, cloudProvider, true)

	panic("unimplemented")

	// Identify the latest Velero Backup.

	// Restore the latest Velero Backup.
}
