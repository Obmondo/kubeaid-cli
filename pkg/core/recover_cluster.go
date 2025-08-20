// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package core

import (
	"context"
	"log/slog"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	awsSDKGoV2Config "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	awsServices "github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud/aws/services"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud/azure"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/kubernetes"
)

func RecoverCluster(ctx context.Context, managementClusterName string, skipPRWorkflow bool) {
	switch globals.CloudProviderName {
	case constants.CloudProviderHetzner:
	case constants.CloudProviderBareMetal:
	case constants.CloudProviderLocal:
		panic("unimplemented")

	default:
		assert.AssertNotNil(ctx,
			config.ParsedGeneralConfig.Cloud.DisasterRecovery,
			"disasterRecovery section in the config file, can't be empty",
		)
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

	sealedSecretsKeysBackupsBucketName := config.ParsedGeneralConfig.Cloud.DisasterRecovery.SealedSecretsBackupsBucketName

	switch globals.CloudProviderName {
	case constants.CloudProviderAWS:
		awsSDKConfig, err := awsSDKGoV2Config.LoadDefaultConfig(ctx)
		assert.AssertErrNil(ctx, err, "Failed initiating AWS SDK config")

		s3Client := s3.NewFromConfig(awsSDKConfig)

		awsServices.DownloadS3BucketContents(ctx,
			s3Client,
			sealedSecretsKeysBackupsBucketName,
			true,
		)

	case constants.CloudProviderAzure:
		credentials := azure.GetClientSecretCredentials(ctx)

		blobClient, err := azblob.NewClient(azure.GetStorageAccountURL(), credentials, nil)
		assert.AssertErrNil(ctx, err, "Failed creating Azure Blob client")

		azure.DownloadBlobContainerContents(ctx,
			blobClient,
			sealedSecretsKeysBackupsBucketName,
		)

	default:
		panic("unreachable")
	}

	// Bootstrap the new cluster.
	BootstrapCluster(ctx, BootstrapClusterArgs{
		CreateDevEnvArgs: &CreateDevEnvArgs{
			ManagementClusterName:    managementClusterName,
			SkipMonitoringSetup:      false,
			SkipPRWorkflow:           skipPRWorkflow,
			IsPartOfDisasterRecovery: true,
		},
		SkipClusterctlMove: false,
	})

	kubeconfig := utils.MustGetEnv(constants.EnvNameKubeconfig)
	clusterClient, err := kubernetes.CreateKubernetesClient(ctx, kubeconfig)
	assert.AssertErrNil(ctx, err,
		"Failed creating cluster client",
		slog.String("kubeconfig", kubeconfig),
	)

	// Identify the latest Velero Backup.
	latestVeleroBackup := kubernetes.GetLatestVeleroBackup(ctx, clusterClient)

	// Restore the latest Velero Backup.
	kubernetes.RestoreVeleroBackup(ctx, clusterClient, latestVeleroBackup)

	slog.InfoContext(ctx, "Cluster recovered successfully 🎊")
}
