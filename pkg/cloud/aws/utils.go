// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package aws

import (
	"context"

	awsSDKGoV2Config "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"sigs.k8s.io/cluster-api-provider-aws/v2/cmd/clusterawsadm/cmd/bootstrap/iam"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
)

// Returns the AWS Account ID.
// NOTE : Picks up AWS credentials from the environment.
func GetAccountID(ctx context.Context) string {
	// Load AWS SDK config.
	awsSDKConfig, err := awsSDKGoV2Config.LoadDefaultConfig(ctx)
	assert.AssertErrNil(ctx, err, "Failed initiating AWS SDK config")

	stsClient := sts.NewFromConfig(awsSDKConfig)
	output, err := stsClient.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	assert.AssertErrNil(ctx, err, "Failed getting AWS account ID")

	return *output.Account
}

// Creates / updates the AWS CloudFormation Stack containing necessary IAM role-policies, required
// by ClusterAPI and the EC2 instance of the provisioned cluster.
func CreateIAMCloudFormationStack(ctx context.Context) {
	// The clusterawsadm CLI utility picks up the credentials that you set as environment variables
	// and uses them to create the CloudFormation stack.
	// NOTE : This requires admin privileges.
	iamCmd := iam.RootCmd()
	iamCmd.SetArgs([]string{
		"create-cloudformation-stack",
	})
	err := iamCmd.ExecuteContext(ctx)

	// NOTE : If the CloudFormation template is in ROLLBACK_COMPLETE state, maybe there are
	//        pre-existing IAM resources with overlapping name. If so, then first delete them
	//        manually from the AWS Console and then retry running the script.
	assert.AssertErrNil(ctx, err, "Failed creating / updating IAM CloudFormation stack")
}
