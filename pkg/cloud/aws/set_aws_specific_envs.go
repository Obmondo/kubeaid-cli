// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package aws

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"sigs.k8s.io/cluster-api-provider-aws/v2/cmd/clusterawsadm/cmd/bootstrap/credentials"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils"
)

var executeCredentialsCmd = func(ctx context.Context) (string, error) {
	credentialsCmdOutputBuffer := new(bytes.Buffer)

	credentialsCmd := credentials.RootCmd()
	credentialsCmd.SetArgs([]string{
		"encode-as-profile",
	})
	credentialsCmd.SetOut(credentialsCmdOutputBuffer)

	if err := credentialsCmd.ExecuteContext(ctx); err != nil {
		return "", err
	}

	return credentialsCmdOutputBuffer.String(), nil
}

// SetAWSSpecificEnvs sets AWS specific environment variables, required by the
// 'clusterawsadm bootstrap iam' command / core.getTemplateValues( ) / AWS SDK.
func SetAWSSpecificEnvs(ctx context.Context) error {
	awsCredentials := config.ParsedSecretsConfig.AWS

	utils.MustSetEnv(constants.EnvNameAWSAccessKey, awsCredentials.AWSAccessKeyID)
	utils.MustSetEnv(constants.EnvNameAWSSecretKey, awsCredentials.AWSSecretAccessKey)
	utils.MustSetEnv(constants.EnvNameAWSSessionToken, awsCredentials.AWSSessionToken)
	utils.MustSetEnv(constants.EnvNameAWSRegion, config.ParsedGeneralConfig.Cloud.AWS.Region)

	credentialsCmdOutput, err := executeCredentialsCmd(ctx)
	if err != nil {
		return fmt.Errorf("creating Base64 encoded credentials for CAPA: %w", err)
	}

	credentialsCmdOutputParts := strings.Split(
		credentialsCmdOutput,
		"WARNING: `encode-as-profile` should only be used for bootstrapping.",
	)
	awsB64EncodedCredentials := strings.TrimSpace(
		credentialsCmdOutputParts[len(credentialsCmdOutputParts)-1],
	)
	utils.MustSetEnv(constants.EnvNameAWSB64EcodedCredentials, awsB64EncodedCredentials)

	return nil
}
