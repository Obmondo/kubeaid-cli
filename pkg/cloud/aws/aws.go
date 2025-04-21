package aws

import (
	"context"
	"fmt"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	awsSDKGoV2Config "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/s3"
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

func (*AWS) GetSealedSecretsBackupBucketName() string {
	return config.ParsedGeneralConfig.Cloud.AWS.DisasterRecovery.SealedSecretsBackupBucketName
}

func (*AWS) UpdateCapiClusterValuesFileWithCloudSpecificDetails(ctx context.Context,
	capiClusterValuesFilePath string,
	_updates any,
) {
	updates, ok := _updates.(MachineTemplateUpdates)
	assert.Assert(ctx, ok, "Wrong type of MachineTemplateUpdates object passed")

	// Update the Control Plane AMI ID.
	_ = utils.ExecuteCommandOrDie(fmt.Sprintf(
		"yq -i -y '(.aws.controlPlane.ami.id) = \"%s\"' %s",
		updates.AMIID, capiClusterValuesFilePath,
	))

	// Update AMI ID in each node-group definition.
	_ = utils.ExecuteCommandOrDie(fmt.Sprintf(
		"yq -i -y '(.aws.nodeGroups[].ami.id) = \"%s\"' %s",
		updates.AMIID, capiClusterValuesFilePath,
	))
}
