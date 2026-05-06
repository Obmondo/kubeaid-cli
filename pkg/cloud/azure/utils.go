// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package azure

import (
	"fmt"
	"net/url"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
)

// Constructs and returns Azure client secret credentials.
func GetClientSecretCredentials() (*azidentity.ClientSecretCredential, error) {
	credentials, err := azidentity.NewClientSecretCredential(
		config.ParsedGeneralConfig.Cloud.Azure.TenantID,
		config.ParsedSecretsConfig.Azure.ClientID,
		config.ParsedSecretsConfig.Azure.ClientSecret,
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("constructing Azure credentials: %w", err)
	}

	return credentials, nil
}

// Type casts the given CloudProvider interface instance to an instance of the Azure struct.
func CloudProviderToAzure(cloudProvider cloud.CloudProvider) (*Azure, error) {
	azure, ok := cloudProvider.(*Azure)
	if !ok {
		return nil, fmt.Errorf("failed type casting CloudProvider interface to Azure struct type")
	}

	return azure, nil
}

// Returns URL of the storage account being used.
func GetStorageAccountURL() string {
	return fmt.Sprintf("https://%s.blob.core.windows.net/",
		config.ParsedGeneralConfig.Cloud.Azure.StorageAccount,
	)
}

// Returns URL of the OIDC provider being used for Workload Identity support.
func GetServiceAccountIssuerURL() (string, error) {
	storageAccountURL := fmt.Sprintf(
		"https://%s.blob.core.windows.net/",
		config.ParsedGeneralConfig.Cloud.Azure.StorageAccount,
	)

	serviceAccountIssuerURL, err := url.JoinPath(
		storageAccountURL,
		constants.BlobContainerNameOIDCProvider,
	)
	if err != nil {
		return "", fmt.Errorf("constructing ServiceAccount issuer URL: %w", err)
	}

	return serviceAccountIssuerURL, nil
}
