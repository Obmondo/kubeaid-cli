// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package aws

import (
	"context"

	awsSDKGoV2Config "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
)

type AWS struct {
	iamClient *iam.Client
	s3Client  *s3.Client
	ec2Client *ec2.Client
}

func NewAWSCloudProvider() cloud.CloudProvider {
	ctx := context.Background()

	// Load AWS SDK config.
	awsSDKConfig, err := awsSDKGoV2Config.LoadDefaultConfig(ctx)
	assert.AssertErrNil(ctx, err, "Failed initiating AWS SDK config")

	return &AWS{
		iamClient: iam.NewFromConfig(awsSDKConfig),
		s3Client:  s3.NewFromConfig(awsSDKConfig),
		ec2Client: ec2.NewFromConfig(awsSDKConfig),
	}
}
