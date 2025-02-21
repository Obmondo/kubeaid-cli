package aws

import (
	"context"
	"log/slog"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
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

// Creates / updates the AWS CloudFormation Stack containing necessary IAM role-policies, required
// by ClusterAPI and the EC2 instance of the provisioned cluster.
func CreateIAMCloudFormationStack() {
	// The clusterawsadm CLI utility picks up the credentials that you set as environment variables
	// and uses them to create the CloudFormation stack.
	// NOTE : This requires admin privileges.
	output, err := utils.ExecuteCommand("clusterawsadm bootstrap iam create-cloudformation-stack")

	// NOTE : If the CloudFormation template is in ROLLBACK_COMPLETE state, maybe there are
	// pre-existing IAM resources with overlapping name. If so, then first delete them manually from
	// the AWS Console and then retry running the script.
	assert.AssertErrNil(context.Background(), err,
		"Failed bootstrapping IAM CloudFormation Stack", slog.String("output", output),
	)
}
