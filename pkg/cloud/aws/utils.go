// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package aws

import (
	"context"
	"fmt"

	awsSDKGoV2Config "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"sigs.k8s.io/cluster-api-provider-aws/v2/cmd/clusterawsadm/cmd/bootstrap/iam"
)

var getCallerIdentity = func(ctx context.Context) (*sts.GetCallerIdentityOutput, error) {
	awsSDKConfig, err := awsSDKGoV2Config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("initiating AWS SDK config: %w", err)
	}

	stsClient := sts.NewFromConfig(awsSDKConfig)

	return stsClient.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
}

// GetAccountID returns the AWS Account ID.
// NOTE: Picks up AWS credentials from the environment.
func GetAccountID(ctx context.Context) (string, error) {
	output, err := getCallerIdentity(ctx)
	if err != nil {
		return "", fmt.Errorf("getting AWS account ID: %w", err)
	}

	return *output.Account, nil
}

var executeIAMBootstrapCmd = func(ctx context.Context) error {
	iamCmd := iam.RootCmd()
	iamCmd.SetArgs([]string{
		"create-cloudformation-stack",
	})

	return iamCmd.ExecuteContext(ctx)
}

// CreateIAMCloudFormationStack creates / updates the AWS CloudFormation Stack containing necessary
// IAM role-policies, required by ClusterAPI and the EC2 instance of the provisioned cluster.
func CreateIAMCloudFormationStack(ctx context.Context) error {
	if err := executeIAMBootstrapCmd(ctx); err != nil {
		return fmt.Errorf("creating/updating IAM CloudFormation stack: %w", err)
	}

	return nil
}
