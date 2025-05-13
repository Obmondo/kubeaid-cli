package aws

import (
	"context"
	"fmt"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud/aws/services"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
)

func getIAMTrustPolicy(ctx context.Context) services.PolicyDocument {
	return services.PolicyDocument{
		Version: "2012-10-17",
		Statement: []services.PolicyStatement{
			{
				Action: []string{"sts:AssumeRole"},
				Effect: "Allow",
				Principal: map[string]string{
					"AWS": fmt.Sprintf(
						"arn:aws:iam::%s:role/nodes.cluster-api-provider-aws.sigs.k8s.io",
						GetAccountID(ctx),
					),
				},
			},
		},
	}
}

func getSealedSecretsBackuperIAMPolicy() services.PolicyDocument {
	sealedSecretBackupsBucketName := config.ParsedGeneralConfig.Cloud.DisasterRecovery.SealedSecretsBackupsBucketName

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
				Resource: fmt.Sprintf("arn:aws:s3:::%s/*", sealedSecretBackupsBucketName),
			},
		},
	}
}

func getVeleroIAMPolicy() services.PolicyDocument {
	veleroBackupsS3BucketName := config.ParsedGeneralConfig.Cloud.DisasterRecovery.VeleroBackupsBucketName

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
