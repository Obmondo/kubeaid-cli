package azure

import (
	"context"
	"fmt"
	"net/url"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
)

// Constructs and returns Azure client secret credentials.
func GetClientSecretCredentials(ctx context.Context) *azidentity.ClientSecretCredential {
	credentials, err := azidentity.NewClientSecretCredential(
		config.ParsedGeneralConfig.Cloud.Azure.TenantID,
		config.ParsedSecretsConfig.Azure.ClientID,
		config.ParsedSecretsConfig.Azure.ClientSecret,
		nil,
	)
	assert.AssertErrNil(ctx, err, "Failed constructing Azure credentials")

	return credentials
}

// Type casts the give CloudProvider interface instance to an instance of the Azure struct.
// Panics if the type casting fails.
func CloudProviderToAzure(ctx context.Context, cloudProvider cloud.CloudProvider) *Azure {
	azure, ok := cloudProvider.(*Azure)
	assert.Assert(ctx, ok, "Failed type casting CloudProvider interface to Azure struct type")

	return azure
}

// Returns URL of the storage account being used.
func GetStorageAccountURL() string {
	return fmt.Sprintf("https://%s.blob.core.windows.net/",
		config.ParsedGeneralConfig.Cloud.Azure.StorageAccount,
	)
}

// Returns URL of the external OpenID provider being used for Workload Identity support.
func GetServiceAccountIssuerURL(ctx context.Context) string {
	storageAccountURL := fmt.Sprintf(
		"https://%s.blob.core.windows.net/",
		config.ParsedGeneralConfig.Cloud.Azure.StorageAccount,
	)

	serviceAccountIssuerURL, err := url.JoinPath(
		storageAccountURL,
		constants.BlobContainerNameWorkloadIdentity,
	)
	assert.AssertErrNil(ctx, err, "Failed constructing ServiceAccount issuer URL")

	return serviceAccountIssuerURL
}
