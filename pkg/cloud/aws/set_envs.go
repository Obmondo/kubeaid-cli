package aws

import (
	"os"
	"strings"

	"github.com/Obmondo/kubeaid-bootstrap-script/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/utils"
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
			utils.ExecuteCommandOrDie("clusterawsadm bootstrap credentials encode-as-profile"),
			"WARNING: `encode-as-profile` should only be used for bootstrapping.",
		)[1],
	)
	os.Setenv(constants.EnvNameAWSB64EcodedCredentials, awsB64EncodedCredentials)
}
