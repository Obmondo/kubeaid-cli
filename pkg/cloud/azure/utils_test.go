// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package azure

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
)

// Mutates config.ParsedGeneralConfig — sequential only.
func TestGetStorageAccountURL(t *testing.T) {
	tests := []struct {
		name           string
		storageAccount string
		want           string
	}{
		{
			name:           "standard storage account name",
			storageAccount: "mystorageaccount",
			want:           "https://mystorageaccount.blob.core.windows.net/",
		},
		{
			name:           "alphanumeric storage account name",
			storageAccount: "storage123abc",
			want:           "https://storage123abc.blob.core.windows.net/",
		},
		{
			name:           "empty storage account name",
			storageAccount: "",
			want:           "https://.blob.core.windows.net/",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			saved := config.ParsedGeneralConfig
			t.Cleanup(func() { config.ParsedGeneralConfig = saved })

			config.ParsedGeneralConfig = &config.GeneralConfig{
				Cloud: config.CloudConfig{
					Azure: &config.AzureConfig{
						StorageAccount: tc.storageAccount,
					},
				},
			}

			got := GetStorageAccountURL()
			assert.Equal(t, tc.want, got)
		})
	}
}

// Mutates config.ParsedGeneralConfig — sequential only.
func TestGetServiceAccountIssuerURL(t *testing.T) {
	tests := []struct {
		name           string
		storageAccount string
		want           string
		wantErr        bool
	}{
		{
			name:           "valid storage account",
			storageAccount: "mystorageaccount",
			want:           "https://mystorageaccount.blob.core.windows.net/" + constants.BlobContainerNameOIDCProvider,
		},
		{
			name:           "different storage account",
			storageAccount: "prodaccount",
			want:           "https://prodaccount.blob.core.windows.net/" + constants.BlobContainerNameOIDCProvider,
		},
		{
			name:           "empty storage account",
			storageAccount: "",
			want:           "https://.blob.core.windows.net/" + constants.BlobContainerNameOIDCProvider,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			saved := config.ParsedGeneralConfig
			t.Cleanup(func() { config.ParsedGeneralConfig = saved })

			config.ParsedGeneralConfig = &config.GeneralConfig{
				Cloud: config.CloudConfig{
					Azure: &config.AzureConfig{
						StorageAccount: tc.storageAccount,
					},
				},
			}

			got, err := GetServiceAccountIssuerURL()
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
