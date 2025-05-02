package aws

import (
	"strings"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils"
)

// Sets AWS specific environment variables, required by the 'clusterawsadm bootstrap iam' command /
// core.getTemplateValues( ) / AWS SDK.
func SetAWSSpecificEnvs() {
	awsCredentials := config.ParsedSecretsConfig.AWS

	utils.MustSetEnv(constants.EnvNameAWSAccessKey, awsCredentials.AWSAccessKeyID)
	utils.MustSetEnv(constants.EnvNameAWSSecretKey, awsCredentials.AWSSecretAccessKey)
	utils.MustSetEnv(constants.EnvNameAWSSessionToken, awsCredentials.AWSSessionToken)
	utils.MustSetEnv(constants.EnvNameAWSRegion, config.ParsedGeneralConfig.Cloud.AWS.Region)

	awsB64EncodedCredentials := strings.TrimSpace(
		strings.Split(
			utils.ExecuteCommandOrDie("clusterawsadm bootstrap credentials encode-as-profile"),
			"WARNING: `encode-as-profile` should only be used for bootstrapping.",
		)[1],
	)
	utils.MustSetEnv(constants.EnvNameAWSB64EcodedCredentials, awsB64EncodedCredentials)
}
