package aws

import (
	"context"

	"github.com/Obmondo/kubeaid-bootstrap-script/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/utils/assert"
	awsSDKGoV2Config "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type AWS struct {
	s3Client  *s3.Client
	iamClient *iam.Client
}

func NewAWSCloudProvider() *AWS {
	ctx := context.Background()

	// Load AWS SDK config.
	awsSDKConfig, err := awsSDKGoV2Config.LoadDefaultConfig(ctx)
	assert.AssertErrNil(ctx, err, "Failed initiating AWS SDK config")

	return &AWS{
		s3Client:  s3.NewFromConfig(awsSDKConfig),
		iamClient: iam.NewFromConfig(awsSDKConfig),
	}
}

func (*AWS) GetSealedSecretsBackupBucketName() string {
	return config.ParsedConfig.Cloud.AWS.DisasterRecovery.SealedSecretsBackupS3BucketName
}
