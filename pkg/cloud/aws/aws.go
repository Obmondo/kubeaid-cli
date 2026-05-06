// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsSDKGoV2Config "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud/aws/services"
)

type AWS struct {
	iamClient services.IAMAPI
	s3Client  services.S3API
	ec2Client ec2DescribeInstanceTypesAPI
}

var loadAWSConfig = func(ctx context.Context) (aws.Config, error) {
	return awsSDKGoV2Config.LoadDefaultConfig(ctx)
}

func NewAWSCloudProvider() (cloud.CloudProvider, error) {
	ctx := context.Background()

	awsSDKConfig, err := loadAWSConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("initiating AWS SDK config: %w", err)
	}

	return &AWS{
		iamClient: iam.NewFromConfig(awsSDKConfig),
		s3Client:  s3.NewFromConfig(awsSDKConfig),
		ec2Client: ec2.NewFromConfig(awsSDKConfig),
	}, nil
}
