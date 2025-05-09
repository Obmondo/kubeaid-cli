package core

import (
	"context"

	awsSDKGoV2Config "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	awsServices "github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud/aws/services"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
)

func RecoverCluster(ctx context.Context, managementClusterName string, skipPRFlow bool) {
	switch globals.CloudProviderName {
	case constants.CloudProviderAWS:
		assert.AssertNotNil(ctx,
			config.ParsedGeneralConfig.Cloud.AWS.DisasterRecovery,
			"disasterRecovery section in the config file, can't be empty",
		)

	case constants.CloudProviderAzure:
		panic("unimplemented")

	case constants.CloudProviderHetzner:
		assert.AssertNil(ctx,
			config.ParsedGeneralConfig.Cloud.Hetzner,
			"Disaster recovery isn't supported for Hetzner",
		)

	default:
		panic("unreachable")
	}

	/*
		Pull and gzip decode backed up (by Sealed Secrets backuper CRONJob) Kubernetes Secrets from S3
		bucket. Each Kubernetes Secret contains a Sealed Secrets encryption key.

		The script responsible for this backup process can be found here :
		https://github.com/Obmondo/kubeaid/blob/master/argocd-helm-charts/sealed-secrets/templates/configmap.yaml

		And you can read about Sealed Secrets key rotation from these references :
			(1) https://playbook.stakater.com/content/workshop/sealed-secrets/management.html.
			(2) https://github.com/bitnami-labs/sealed-secrets?tab=readme-ov-file#secret-rotation
	*/
	switch globals.CloudProviderName {
	case constants.CloudProviderAWS:
		awsSDKConfig, err := awsSDKGoV2Config.LoadDefaultConfig(ctx)
		assert.AssertErrNil(ctx, err, "Failed initiating AWS SDK config")

		s3Client := s3.NewFromConfig(awsSDKConfig)

		sealedSecretsKeysBackupBucketName := config.ParsedGeneralConfig.Cloud.AWS.DisasterRecovery.SealedSecretsBackupBucketName
		awsServices.DownloadS3BucketContents(ctx, s3Client, sealedSecretsKeysBackupBucketName, true)

	case constants.CloudProviderHetzner:
		panic("unimplemented")

	case constants.CloudProviderAzure:

	default:
		panic("unreachable")
	}

	// Bootstrap the new cluster.
	BootstrapCluster(ctx, BootstrapClusterArgs{
		CreateDevEnvArgs: &CreateDevEnvArgs{
			ManagementClusterName:    managementClusterName,
			SkipMonitoringSetup:      false,
			SkipKubePrometheusBuild:  false,
			SkipPRFlow:               skipPRFlow,
			IsPartOfDisasterRecovery: true,
		},
		SkipClusterctlMove: false,
	})

	panic("unimplemented")

	// Identify the latest Velero Backup.

	// Restore the latest Velero Backup.
}
