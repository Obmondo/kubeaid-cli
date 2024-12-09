package utils

import (
	"context"
	"log/slog"
	"os"
	"strings"

	"github.com/Obmondo/kubeaid-bootstrap-script/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/utils/assert"
)

// Sets AWS specific environment variables, required by the 'clusterawsadm bootstrap iam' command /
// core.getTemplateValues( ) / AWS SDK.
func SetAWSSpecificEnvs() {
	awsCredentials := config.ParsedConfig.Cloud.AWS.Credentials

	os.Setenv(constants.EnvNameAWSAccessKey, awsCredentials.AWSAccessKey)
	os.Setenv(constants.EnvNameAWSSecretKey, awsCredentials.AWSSecretKey)
	os.Setenv(constants.EnvNameAWSSessionToken, awsCredentials.AWSSessionToken)
	os.Setenv(constants.EnvNameAWSRegion, awsCredentials.AWSRegion)

	awsB64EncodedCredentials := strings.TrimSpace(
		strings.Split(
			ExecuteCommandOrDie("clusterawsadm bootstrap credentials encode-as-profile"),
			"WARNING: `encode-as-profile` should only be used for bootstrapping.",
		)[1],
	)
	os.Setenv(constants.EnvNameAWSB64EcodedCredentials, awsB64EncodedCredentials)
}

// Creates / updates the AWS CloudFormation Stack containing necessary IAM role-policies, required
// by ClusterAPI and the EC2 instance of the provisioned cluster.
func CreateIAMCloudFormationStack() {
	// The clusterawsadm CLI utility picks up the credentials that you set as environment variables
	// and uses them to create the CloudFormation stack.
	// NOTE : This requires admin privileges.
	output, err := ExecuteCommand("clusterawsadm bootstrap iam create-cloudformation-stack")

	// Panic if an error occurs (except regarding the AWS Cloudformation stack already existing).
	if !strings.Contains(output, "already exists, updating") {
		assert.AssertErrNil(context.Background(), err, "Failed bootstrapping IAM CloudFormation Stack", slog.String("output", output))
	}
}
