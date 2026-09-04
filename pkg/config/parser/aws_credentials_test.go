// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package parser

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
)

// writeAWSCredentialsFile writes a shared-credentials file with two
// static-credential profiles and points the SDK at it. Static credentials
// resolve entirely from the file, so no AWS call is made.
//
// The path goes through AWS_SHARED_CREDENTIALS_FILE rather than a fake HOME :
// the SDK derives its default ~/.aws paths into package-level vars at init, so
// a HOME set afterwards is never consulted.
func writeAWSCredentialsFile(t *testing.T) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "credentials")
	require.NoError(t, os.WriteFile(path, []byte(`[default]
aws_access_key_id = AKIADEFAULT
aws_secret_access_key = default-secret

[work]
aws_access_key_id = AKIAWORK
aws_secret_access_key = work-secret
aws_session_token = work-token
`), 0o600))

	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", path)
	// Nothing from the developer's shell may steer resolution.
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(t.TempDir(), "config"))
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	t.Setenv("AWS_SESSION_TOKEN", "")
}

// hydrateAWSCredentials is what makes issue #87's `aws.profile` mean anything :
// it is the one place where "which AWS account is this cluster in" is settled.
func TestHydrateAWSCredentials(t *testing.T) {
	tests := []struct {
		name             string
		secrets          *config.AWSCredentials
		wantAccessKeyID  string
		wantSecretKey    string
		wantSessionToken string
	}{
		{
			name:            "no aws block falls back to the SDK default profile",
			secrets:         nil,
			wantAccessKeyID: "AKIADEFAULT",
			wantSecretKey:   "default-secret",
		},
		{
			name:             "a named profile is read instead of the default one",
			secrets:          &config.AWSCredentials{Profile: "work"},
			wantAccessKeyID:  "AKIAWORK",
			wantSecretKey:    "work-secret",
			wantSessionToken: "work-token",
		},
		{
			name: "explicit credentials win over the profile",
			secrets: &config.AWSCredentials{
				Profile:            "work",
				AWSAccessKeyID:     "AKIAEXPLICIT",
				AWSSecretAccessKey: "explicit-secret",
			},
			wantAccessKeyID: "AKIAEXPLICIT",
			wantSecretKey:   "explicit-secret",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			writeAWSCredentialsFile(t)

			saved := config.ParsedSecretsConfig
			t.Cleanup(func() { config.ParsedSecretsConfig = saved })
			config.ParsedSecretsConfig = &config.SecretsConfig{AWS: tc.secrets}

			hydrateAWSCredentials(context.Background())

			got := config.ParsedSecretsConfig.AWS
			require.NotNil(t, got)
			assert.Equal(t, tc.wantAccessKeyID, got.AWSAccessKeyID)
			assert.Equal(t, tc.wantSecretKey, got.AWSSecretAccessKey)
			assert.Equal(t, tc.wantSessionToken, got.AWSSessionToken)
		})
	}
}
