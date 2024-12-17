package aws

import (
	"fmt"

	"github.com/Obmondo/kubeaid-bootstrap-script/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud/aws/services"
)

func getSealedSecretsBackuperIAMPolicy() services.PolicyDocument {
	sealedSecretBackupsS3BucketName := config.ParsedConfig.Cloud.AWS.DisasterRecovery.SealedSecretsBackupS3BucketName

	return services.PolicyDocument{
		Version: "2012-10-17",
		Statement: []services.PolicyStatement{
			{
				Action: []string{
					"s3:PutObject",
					"s3:AbortMultipartUpload",
					"s3:ListMultipartUploadParts",
				},
				Effect:   "Allow",
				Resource: fmt.Sprintf("arn:aws:s3:::-%s", sealedSecretBackupsS3BucketName),
			},
		},
	}
}

func getVeleroIAMPolicy() services.PolicyDocument {
	veleroBackupsS3BucketName := config.ParsedConfig.Cloud.AWS.DisasterRecovery.VeleroBackupsS3BucketName

	return services.PolicyDocument{
		Version: "2012-10-17",
		Statement: []services.PolicyStatement{
			{
				Action: []string{
					"ec2:DescribeVolumes",
					"ec2:DescribeSnapshots",
					"ec2:CreateTags",
					"ec2:CreateVolume",
					"ec2:CreateSnapshot",
					"ec2:DeleteSnapshot",
					"ec2:CopySnapshot",
				},
				Effect:   "Allow",
				Resource: "*",
			},
			{
				Action: []string{
					"s3:GetObject",
					"s3:DeleteObject",
					"s3:PutObject",
					"s3:AbortMultipartUpload",
					"s3:ListMultipartUploadParts",
				},
				Effect:   "Allow",
				Resource: fmt.Sprintf("arn:aws:s3:::%s/*", veleroBackupsS3BucketName),
			},
			{
				Action: []string{
					"s3:ListBucket",
				},
				Effect:   "Allow",
				Resource: fmt.Sprintf("arn:aws:s3:::%s", veleroBackupsS3BucketName),
			},
		},
	}
}
