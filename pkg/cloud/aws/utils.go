package aws

import (
	"context"

	"github.com/Obmondo/kubeaid-bootstrap-script/utils/assert"
	awsSDKGoV2Config "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sts"
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
