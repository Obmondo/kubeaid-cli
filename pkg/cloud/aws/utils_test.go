// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package aws

import (
	"context"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Mutates getCallerIdentity — sequential only.
func TestGetAccountID(t *testing.T) {
	tests := []struct {
		name    string
		stub    func(ctx context.Context) (*sts.GetCallerIdentityOutput, error)
		want    string
		wantErr bool
		errMsg  string
	}{
		{
			name: "returns account ID on success",
			stub: func(_ context.Context) (*sts.GetCallerIdentityOutput, error) {
				acct := "123456789012"
				return &sts.GetCallerIdentityOutput{Account: &acct}, nil
			},
			want: "123456789012",
		},
		{
			name: "returns error when getCallerIdentity fails",
			stub: func(_ context.Context) (*sts.GetCallerIdentityOutput, error) {
				return nil, fmt.Errorf("credentials expired")
			},
			wantErr: true,
			errMsg:  "getting AWS account ID",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			saved := getCallerIdentity
			t.Cleanup(func() { getCallerIdentity = saved })
			getCallerIdentity = tc.stub

			got, err := GetAccountID(context.Background())
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errMsg)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// Mutates executeIAMBootstrapCmd — sequential only.
func TestCreateIAMCloudFormationStack(t *testing.T) {
	tests := []struct {
		name    string
		stub    func(ctx context.Context) error
		wantErr bool
		errMsg  string
	}{
		{
			name: "succeeds when command succeeds",
			stub: func(_ context.Context) error {
				return nil
			},
		},
		{
			name: "returns error when command fails",
			stub: func(_ context.Context) error {
				return fmt.Errorf("access denied")
			},
			wantErr: true,
			errMsg:  "creating/updating IAM CloudFormation stack",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			saved := executeIAMBootstrapCmd
			t.Cleanup(func() { executeIAMBootstrapCmd = saved })
			executeIAMBootstrapCmd = tc.stub

			err := CreateIAMCloudFormationStack(context.Background())
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errMsg)
				return
			}
			require.NoError(t, err)
		})
	}
}
