// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package aws

import (
	"context"
	"fmt"
	"testing"

	awsSDK "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewAWSCloudProvider(t *testing.T) {
	tests := []struct {
		name          string
		loadAWSConfig func(ctx context.Context) (awsSDK.Config, error)
		wantErr       bool
		errMsg        string
	}{
		{
			name: "returns cloud provider on success",
			loadAWSConfig: func(_ context.Context) (awsSDK.Config, error) {
				return awsSDK.Config{Region: "us-east-1"}, nil
			},
		},
		{
			name: "returns error when config load fails",
			loadAWSConfig: func(_ context.Context) (awsSDK.Config, error) {
				return awsSDK.Config{}, fmt.Errorf("no credentials found")
			},
			wantErr: true,
			errMsg:  "initiating AWS SDK config",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			saved := loadAWSConfig
			t.Cleanup(func() { loadAWSConfig = saved })
			loadAWSConfig = tc.loadAWSConfig

			got, err := NewAWSCloudProvider()
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errMsg)
				return
			}
			require.NoError(t, err)
			assert.NotNil(t, got)
		})
	}
}
