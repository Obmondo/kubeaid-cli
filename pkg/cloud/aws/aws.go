// Copyright 2025 Obmondo
// SPDX-License-Identifier: Apache-2.0

package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsSDKGoV2Config "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/Obmondo/kubeaid-cli/pkg/cloud"
	"github.com/Obmondo/kubeaid-cli/pkg/cloud/aws/services"
	"github.com/Obmondo/kubeaid-cli/pkg/config"
)

type AWS struct {
	iamClient services.IAMAPI
	s3Client  services.S3API
	ec2Client ec2DescribeInstanceTypesAPI
}

// SelectedProfile returns the ~/.aws profile chosen in secrets.yaml
// (aws.profile), or "" when the SDK's own default resolution applies.
func SelectedProfile() string {
	if config.ParsedSecretsConfig == nil || config.ParsedSecretsConfig.AWS == nil {
		return ""
	}

	return config.ParsedSecretsConfig.AWS.Profile
}

// LoadSDKConfig builds an AWS SDK config, pinned to the profile selected in
// secrets.yaml when there is one. Every AWS SDK client in kubeaid-cli is built
// from this, so that on a machine carrying several AWS accounts we talk to the
// account the cluster belongs to rather than the SDK's default profile.
func LoadSDKConfig(ctx context.Context) (aws.Config, error) {
	var options []func(*awsSDKGoV2Config.LoadOptions) error

	if profile := SelectedProfile(); profile != "" {
		options = append(options, awsSDKGoV2Config.WithSharedConfigProfile(profile))
	}

	return awsSDKGoV2Config.LoadDefaultConfig(ctx, options...)
}

var loadAWSConfig = LoadSDKConfig

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
