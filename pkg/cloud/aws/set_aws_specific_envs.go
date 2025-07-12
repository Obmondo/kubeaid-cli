package aws

import (
	"bytes"
	"context"
	"strings"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"sigs.k8s.io/cluster-api-provider-aws/v2/cmd/clusterawsadm/cmd/bootstrap/credentials"
)

// Sets AWS specific environment variables, required by the 'clusterawsadm bootstrap iam' command /
// core.getTemplateValues( ) / AWS SDK.
func SetAWSSpecificEnvs(ctx context.Context) {
	awsCredentials := config.ParsedSecretsConfig.AWS

	utils.MustSetEnv(constants.EnvNameAWSAccessKey, awsCredentials.AWSAccessKeyID)
	utils.MustSetEnv(constants.EnvNameAWSSecretKey, awsCredentials.AWSSecretAccessKey)
	utils.MustSetEnv(constants.EnvNameAWSSessionToken, awsCredentials.AWSSessionToken)
	utils.MustSetEnv(constants.EnvNameAWSRegion, config.ParsedGeneralConfig.Cloud.AWS.Region)

	credentialsCmdOutputBuffer := new(bytes.Buffer)

	credentialsCmd := credentials.RootCmd()
	credentialsCmd.SetArgs([]string{
		"encode-as-profile",
	})
	credentialsCmd.SetOut(credentialsCmdOutputBuffer)

	err := credentialsCmd.ExecuteContext(ctx)
	assert.AssertErrNil(ctx, err, "Failed created Base64 encoded credentials for CAPA")

	awsB64EncodedCredentials := strings.TrimSpace(
		strings.Split(
			credentialsCmdOutputBuffer.String(),
			"WARNING: `encode-as-profile` should only be used for bootstrapping.",
		)[1],
	)
	utils.MustSetEnv(constants.EnvNameAWSB64EcodedCredentials, awsB64EncodedCredentials)
}
