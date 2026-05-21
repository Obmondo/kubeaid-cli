// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package aws

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-cli/pkg/cloud/aws/services"
	"github.com/Obmondo/kubeaid-cli/pkg/config"
)

func TestGetIAMTrustPolicy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		accountID string
		want      services.PolicyDocument
	}{
		{
			name:      "produces correct trust policy for a typical account ID",
			accountID: "123456789012",
			want: services.PolicyDocument{
				Version: "2012-10-17",
				Statement: []services.PolicyStatement{
					{
						Action: []string{"sts:AssumeRole"},
						Effect: "Allow",
						Principal: map[string]string{
							"AWS": "arn:aws:iam::123456789012:role/nodes.cluster-api-provider-aws.sigs.k8s.io",
						},
					},
				},
			},
		},
		{
			name:      "empty account ID produces ARN with empty segment",
			accountID: "",
			want: services.PolicyDocument{
				Version: "2012-10-17",
				Statement: []services.PolicyStatement{
					{
						Action: []string{"sts:AssumeRole"},
						Effect: "Allow",
						Principal: map[string]string{
							"AWS": "arn:aws:iam:::role/nodes.cluster-api-provider-aws.sigs.k8s.io",
						},
					},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := getIAMTrustPolicy(tc.accountID)
			assert.Equal(t, tc.want, got)
		})
	}
}

// Mutates config.ParsedGeneralConfig -- sequential only.
func TestGetSealedSecretsBackuperIAMPolicy(t *testing.T) {
	tests := []struct {
		name       string
		bucketName string
		want       services.PolicyDocument
	}{
		{
			name:       "produces correct policy with bucket name",
			bucketName: "my-sealed-secrets-bucket",
			want: services.PolicyDocument{
				Version: "2012-10-17",
				Statement: []services.PolicyStatement{
					{
						Action: []string{
							"s3:PutObject",
							"s3:AbortMultipartUpload",
							"s3:ListMultipartUploadParts",
						},
						Effect:   "Allow",
						Resource: "arn:aws:s3:::my-sealed-secrets-bucket/*",
					},
				},
			},
		},
		{
			name:       "empty bucket name produces ARN with empty segment",
			bucketName: "",
			want: services.PolicyDocument{
				Version: "2012-10-17",
				Statement: []services.PolicyStatement{
					{
						Action: []string{
							"s3:PutObject",
							"s3:AbortMultipartUpload",
							"s3:ListMultipartUploadParts",
						},
						Effect:   "Allow",
						Resource: "arn:aws:s3:::/*",
					},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			originalCfg := config.ParsedGeneralConfig
			t.Cleanup(func() { config.ParsedGeneralConfig = originalCfg })

			config.ParsedGeneralConfig = &config.GeneralConfig{
				Cloud: config.CloudConfig{
					DisasterRecovery: &config.DisasterRecoveryConfig{
						SealedSecretsBackupsBucketName: tc.bucketName,
					},
				},
			}

			got := getSealedSecretsBackuperIAMPolicy()
			assert.Equal(t, tc.want, got)
		})
	}
}

// Mutates config.ParsedGeneralConfig -- sequential only.
func TestGetVeleroIAMPolicy(t *testing.T) {
	tests := []struct {
		name       string
		bucketName string
		want       services.PolicyDocument
	}{
		{
			name:       "produces correct policy with all three statements",
			bucketName: "velero-backups-prod",
			want: services.PolicyDocument{
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
						Resource: fmt.Sprintf("arn:aws:s3:::%s/*", "velero-backups-prod"),
					},
					{
						Action: []string{
							"s3:ListBucket",
						},
						Effect:   "Allow",
						Resource: fmt.Sprintf("arn:aws:s3:::%s", "velero-backups-prod"),
					},
				},
			},
		},
		{
			name:       "empty bucket name produces ARN with empty segment",
			bucketName: "",
			want: services.PolicyDocument{
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
						Resource: "arn:aws:s3:::/*",
					},
					{
						Action: []string{
							"s3:ListBucket",
						},
						Effect:   "Allow",
						Resource: "arn:aws:s3:::",
					},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			originalCfg := config.ParsedGeneralConfig
			t.Cleanup(func() { config.ParsedGeneralConfig = originalCfg })

			config.ParsedGeneralConfig = &config.GeneralConfig{
				Cloud: config.CloudConfig{
					DisasterRecovery: &config.DisasterRecoveryConfig{
						VeleroBackupsBucketName: tc.bucketName,
					},
				},
			}

			got := getVeleroIAMPolicy()
			require.Len(t, got.Statement, 3)
			assert.Equal(t, tc.want, got)
		})
	}
}
