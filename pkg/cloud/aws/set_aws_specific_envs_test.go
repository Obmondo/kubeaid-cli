// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package aws

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
)

// Mutates executeCredentialsCmd, config.ParsedSecretsConfig, config.ParsedGeneralConfig, env vars — sequential only.
func TestSetAWSSpecificEnvs(t *testing.T) {
	envVarsToRestore := []string{
		constants.EnvNameAWSAccessKey,
		constants.EnvNameAWSSecretKey,
		constants.EnvNameAWSSessionToken,
		constants.EnvNameAWSRegion,
		constants.EnvNameAWSB64EcodedCredentials,
	}

	tests := []struct {
		name    string
		stub    func(ctx context.Context) (string, error)
		wantErr bool
		errMsg  string
		wantB64 string
	}{
		{
			name: "sets all env vars on success",
			stub: func(_ context.Context) (string, error) {
				return "WARNING: `encode-as-profile` should only be used for bootstrapping.\n  base64credentials  \n", nil
			},
			wantB64: "base64credentials",
		},
		{
			name: "sets env vars when output has no warning prefix",
			stub: func(_ context.Context) (string, error) {
				return "rawcredentials", nil
			},
			wantB64: "rawcredentials",
		},
		{
			name: "returns error when credentials command fails",
			stub: func(_ context.Context) (string, error) {
				return "", fmt.Errorf("command failed")
			},
			wantErr: true,
			errMsg:  "creating Base64 encoded credentials for CAPA",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			savedCmd := executeCredentialsCmd
			savedSecrets := config.ParsedSecretsConfig
			savedGeneral := config.ParsedGeneralConfig
			t.Cleanup(func() {
				executeCredentialsCmd = savedCmd
				config.ParsedSecretsConfig = savedSecrets
				config.ParsedGeneralConfig = savedGeneral
			})

			savedEnvs := make(map[string]string)
			for _, envVar := range envVarsToRestore {
				savedEnvs[envVar] = os.Getenv(envVar)
			}
			t.Cleanup(func() {
				for _, envVar := range envVarsToRestore {
					if savedEnvs[envVar] == "" {
						_ = os.Unsetenv(envVar)
					} else {
						_ = os.Setenv(envVar, savedEnvs[envVar])
					}
				}
			})

			config.ParsedSecretsConfig = &config.SecretsConfig{
				AWS: &config.AWSCredentials{
					AWSAccessKeyID:     "AKIAIOSFODNN7EXAMPLE",
					AWSSecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
					AWSSessionToken:    "test-session-token",
				},
			}
			config.ParsedGeneralConfig = &config.GeneralConfig{
				Cloud: config.CloudConfig{
					AWS: &config.AWSConfig{
						Region: "eu-west-1",
					},
				},
			}
			executeCredentialsCmd = tc.stub

			err := SetAWSSpecificEnvs(context.Background())
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errMsg)
				return
			}
			require.NoError(t, err)

			assert.Equal(t, "AKIAIOSFODNN7EXAMPLE", os.Getenv(constants.EnvNameAWSAccessKey))
			assert.Equal(t, "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", os.Getenv(constants.EnvNameAWSSecretKey))
			assert.Equal(t, "test-session-token", os.Getenv(constants.EnvNameAWSSessionToken))
			assert.Equal(t, "eu-west-1", os.Getenv(constants.EnvNameAWSRegion))
			assert.Equal(t, tc.wantB64, os.Getenv(constants.EnvNameAWSB64EcodedCredentials))
		})
	}
}
