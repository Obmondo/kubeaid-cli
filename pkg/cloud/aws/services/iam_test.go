// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package services

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud/aws/services/fake"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
)

func TestJsonMarshalIAMPolicyDocument(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		policyDocument PolicyDocument
		wantErr        bool
		validate       func(t *testing.T, result *string)
	}{
		{
			name: "valid policy document with single statement",
			policyDocument: PolicyDocument{
				Version: "2012-10-17",
				Statement: []PolicyStatement{
					{
						Effect:   "Allow",
						Action:   []string{"s3:GetObject"},
						Resource: "arn:aws:s3:::my-bucket/*",
					},
				},
			},
			validate: func(t *testing.T, result *string) {
				t.Helper()
				require.NotNil(t, result)

				var parsed PolicyDocument
				err := json.Unmarshal([]byte(*result), &parsed)
				require.NoError(t, err)

				assert.Equal(t, "2012-10-17", parsed.Version)
				require.Len(t, parsed.Statement, 1)
				assert.Equal(t, "Allow", parsed.Statement[0].Effect)
				assert.Equal(t, []string{"s3:GetObject"}, parsed.Statement[0].Action)
				assert.Equal(t, "arn:aws:s3:::my-bucket/*", parsed.Statement[0].Resource)
			},
		},
		{
			name: "valid policy document with principal",
			policyDocument: PolicyDocument{
				Version: "2012-10-17",
				Statement: []PolicyStatement{
					{
						Effect: "Allow",
						Action: []string{"sts:AssumeRole"},
						Principal: map[string]string{
							"AWS": "arn:aws:iam::123456789012:role/some-role",
						},
					},
				},
			},
			validate: func(t *testing.T, result *string) {
				t.Helper()
				require.NotNil(t, result)

				var parsed PolicyDocument
				err := json.Unmarshal([]byte(*result), &parsed)
				require.NoError(t, err)

				require.Len(t, parsed.Statement, 1)
				assert.Equal(t, "arn:aws:iam::123456789012:role/some-role", parsed.Statement[0].Principal["AWS"])
			},
		},
		{
			name: "valid policy document with multiple statements",
			policyDocument: PolicyDocument{
				Version: "2012-10-17",
				Statement: []PolicyStatement{
					{
						Effect:   "Allow",
						Action:   []string{"ec2:DescribeVolumes", "ec2:CreateSnapshot"},
						Resource: "*",
					},
					{
						Effect:   "Allow",
						Action:   []string{"s3:PutObject"},
						Resource: "arn:aws:s3:::backup-bucket/*",
					},
				},
			},
			validate: func(t *testing.T, result *string) {
				t.Helper()
				require.NotNil(t, result)

				var parsed PolicyDocument
				err := json.Unmarshal([]byte(*result), &parsed)
				require.NoError(t, err)

				assert.Equal(t, "2012-10-17", parsed.Version)
				require.Len(t, parsed.Statement, 2)
				assert.Equal(t, "*", parsed.Statement[0].Resource)
				assert.Equal(t, "arn:aws:s3:::backup-bucket/*", parsed.Statement[1].Resource)
			},
		},
		{
			name: "empty statement list produces valid JSON",
			policyDocument: PolicyDocument{
				Version:   "2012-10-17",
				Statement: []PolicyStatement{},
			},
			validate: func(t *testing.T, result *string) {
				t.Helper()
				require.NotNil(t, result)

				var parsed PolicyDocument
				err := json.Unmarshal([]byte(*result), &parsed)
				require.NoError(t, err)

				assert.Equal(t, "2012-10-17", parsed.Version)
				assert.Empty(t, parsed.Statement)
			},
		},
		{
			name:           "zero-value policy document produces valid JSON",
			policyDocument: PolicyDocument{},
			validate: func(t *testing.T, result *string) {
				t.Helper()
				require.NotNil(t, result)

				var parsed PolicyDocument
				err := json.Unmarshal([]byte(*result), &parsed)
				require.NoError(t, err)

				assert.Empty(t, parsed.Version)
				assert.Nil(t, parsed.Statement)
			},
		},
		{
			name: "omits principal when empty",
			policyDocument: PolicyDocument{
				Version: "2012-10-17",
				Statement: []PolicyStatement{
					{
						Effect:   "Deny",
						Action:   []string{"s3:DeleteObject"},
						Resource: "arn:aws:s3:::important-bucket/*",
					},
				},
			},
			validate: func(t *testing.T, result *string) {
				t.Helper()
				require.NotNil(t, result)

				assert.NotContains(t, *result, "Principal")
			},
		},
		{
			name: "omits resource when empty",
			policyDocument: PolicyDocument{
				Version: "2012-10-17",
				Statement: []PolicyStatement{
					{
						Effect: "Allow",
						Action: []string{"sts:AssumeRole"},
						Principal: map[string]string{
							"AWS": "arn:aws:iam::111111111111:root",
						},
					},
				},
			},
			validate: func(t *testing.T, result *string) {
				t.Helper()
				require.NotNil(t, result)

				assert.NotContains(t, *result, "Resource")
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := jsonMarshalIAMPolicyDocument(tc.policyDocument)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			tc.validate(t, got)
		})
	}
}

// Mutates config.ParsedGeneralConfig — sequential only.
func TestCreateIAMRoleForPolicy(t *testing.T) {
	policyDoc := PolicyDocument{
		Version: "2012-10-17",
		Statement: []PolicyStatement{
			{
				Effect:   "Allow",
				Action:   []string{"s3:GetObject"},
				Resource: "arn:aws:s3:::my-bucket/*",
			},
		},
	}
	assumeDoc := PolicyDocument{
		Version: "2012-10-17",
		Statement: []PolicyStatement{
			{
				Effect:    "Allow",
				Action:    []string{"sts:AssumeRole"},
				Principal: map[string]string{"AWS": "arn:aws:iam::123456789012:root"},
			},
		},
	}

	tests := []struct {
		name      string
		iamClient *fake.IAMAPI
		wantErr   bool
		errMsg    string
	}{
		{
			name:      "all three calls succeed",
			iamClient: &fake.IAMAPI{},
		},
		{
			name: "policy already exists succeeds",
			iamClient: &fake.IAMAPI{
				CreatePolicyErr: fmt.Errorf("policy already exists"),
			},
		},
		{
			name: "role already exists succeeds",
			iamClient: &fake.IAMAPI{
				CreateRoleErr: fmt.Errorf("role already exists"),
			},
		},
		{
			name: "CreatePolicy returns other error",
			iamClient: &fake.IAMAPI{
				CreatePolicyErr: fmt.Errorf("access denied"),
			},
			wantErr: true,
			errMsg:  "creating IAM Policy",
		},
		{
			name: "CreateRole returns other error",
			iamClient: &fake.IAMAPI{
				CreateRoleErr: fmt.Errorf("access denied"),
			},
			wantErr: true,
			errMsg:  "creating IAM Role",
		},
		{
			name: "AttachRolePolicy returns error",
			iamClient: &fake.IAMAPI{
				AttachRolePolicyErr: fmt.Errorf("limit exceeded"),
			},
			wantErr: true,
			errMsg:  "attaching IAM Role and Policy",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			savedCfg := config.ParsedGeneralConfig
			t.Cleanup(func() { config.ParsedGeneralConfig = savedCfg })

			config.ParsedGeneralConfig = &config.GeneralConfig{
				Cluster: config.ClusterConfig{Name: "test-cluster"},
			}

			err := CreateIAMRoleForPolicy(
				context.Background(),
				"123456789012",
				tc.iamClient,
				"test-role",
				policyDoc,
				assumeDoc,
			)
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errMsg)
				return
			}
			require.NoError(t, err)
		})
	}
}
