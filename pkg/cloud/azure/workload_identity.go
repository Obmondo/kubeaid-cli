package azure

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/storage/armstorage"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud/azure/services"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	templateUtils "github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/templates"

	_ "embed"
)

//go:embed templates/*
var templates embed.FS

type TemplateArgs struct {
	StorageAccountName,
	BlobContainerName string
}

/*
Workloads deployed in Kubernetes clusters require Azure AD application credentials or managed
identities to access Azure AD protected resources, such as Azure Key Vault and Microsoft Graph.

The Azure AD Pod Identity open-source project provided a way to avoid needing these secrets, by
using Azure managed identities.

Azure AD Workload Identity for Kubernetes integrates with the capabilities native to Kubernetes to
federate with external identity providers. This approach is simpler to use and deploy, and
overcomes several limitations in Azure AD Pod Identity :

	(1) Removes the scale and performance issues that existed for identity assignment

	(2) Supports Kubernetes clusters hosted in any cloud or on-premises

	(3) Supports both Linux and Windows workloads

	(4) Removes the need for Custom Resource Definitions and pods that intercept Instance Metadata
	    Service (IMDS) traffic

	(5) Avoids the complication and error-prone installation steps such as cluster role assignment
	    from the previous iteration.

In this model, the Kubernetes cluster becomes a token issuer, issuing tokens to Kubernetes Service
Accounts. These service account tokens can be configured to be trusted on Azure AD applications or
user-assigned managed identities.
A workload can exchange a service account token projected to its volume for an Azure AD access
token using the Azure Identity SDKs or the Microsoft Authentication Library (MSAL).

You can read more here : https://azure.github.io/azure-workload-identity/docs/.
*/
func (a *Azure) SetupWorkloadIdentityProvider(ctx context.Context) {
	/*
		(1) The Kubernetes workload sends the signed ServiceAccount token in a request, to Azure Active
		    Directory (AAD).

		(2) AAD will then extract the OpenID provider issuer discovery document URL from the request
		    and fetch it from Azure Storage Container.

		(3) AAD will extract the JWKS document URL from that OpenID provider issuer discovery document
		    and fetch it as well.

		    The JSON Web Key Sets (JWKS) document contains the public signing key(s) that allows AAD to
		    verify the authenticity of the service account token.

		(4) AAD will use the public signing key(s) to verify the authenticity of the ServiceAccount
		    token.

		    Finally it'll return an AAD token, back to the Kubernetes workload.

		You can view the sequence diagram here : https://azure.github.io/azure-workload-identity/docs/installation/self-managed-clusters/oidc-issuer.html#sequence-diagram.
	*/

	subscriptionID := config.ParsedConfig.Cloud.Azure.SubscriptionID

	// Create Azure Resource Group, if it doesn't already exist.

	resourceGroupName := config.ParsedConfig.Cluster.Name

	armClientFactory, err := armresources.NewClientFactory(subscriptionID, a.credentials, nil)
	assert.AssertErrNil(ctx, err, "Failed constructing Azure Resource Manager (ARM) client factory")

	resourceGroupsClient := armClientFactory.NewResourceGroupsClient()

	_, err = resourceGroupsClient.CreateOrUpdate(ctx, resourceGroupName,
		armresources.ResourceGroup{
			Location: &config.ParsedConfig.Cloud.Azure.Location,
		},
		nil,
	)
	assert.AssertErrNil(ctx, err,
		"Failed creating / updating Resource Group",
		slog.String("name", resourceGroupName),
	)

	// Create Azure Storage Account, if it doesn't already exist.

	storageClientFactory, err := armstorage.NewClientFactory(subscriptionID, a.credentials, nil)
	assert.AssertErrNil(ctx, err, "Failed creating Azure Storage client factory")

	storageAccountName := config.ParsedConfig.Cloud.Azure.StorageAccountName

	services.CreateStorageAccount(ctx, &services.CreateStorageAccountArgs{
		StorageAccountsClient: storageClientFactory.NewAccountsClient(),
		ResourceGroupName:     resourceGroupName,
		StorageAccountName:    storageAccountName,
	})

	// Create Azure Storage Container, if it doesn't already exist.
	services.CreateBlobContainer(ctx, &services.CreateBlobContainerArgs{
		ResourceGroupName:    resourceGroupName,
		StorageAccountName:   storageAccountName,
		BlobContainersClient: storageClientFactory.NewBlobContainersClient(),
		BlobContainerName:    constants.WorkloadIdentityBlobContainerName,
	})

	storageAccountURL := fmt.Sprintf("https://%s.blob.core.windows.net/", storageAccountName)

	blobClient, err := azblob.NewClient(storageAccountURL, a.credentials, nil)
	assert.AssertErrNil(ctx, err, "Failed creating Azure Blob client")

	{
		// Generate the OpenID provider issuer discovery document.
		// You can read more about OpenID provider issuer discovery document here :
		// https://openid.net/specs/openid-connect-discovery-1_0.html.
		openIDConfig := templateUtils.ParseAndExecuteTemplate(ctx,
			&templates,
			constants.TemplateNameOpenIDConfig,
			&TemplateArgs{
				StorageAccountName: storageAccountName,
				BlobContainerName:  constants.WorkloadIdentityBlobContainerName,
			},
		)

		// Upload the OpenID provider issuer discovery document to the Azure Storage Container,
		// at path .well-known/openid-configuration.
		_, err := blobClient.UploadBuffer(ctx,
			constants.WorkloadIdentityBlobContainerName,
			constants.AzureBlobNameOpenIDConfiguration,
			openIDConfig,
			nil,
		)
		assert.AssertErrNil(ctx, err, "Failed uploading openid-configuration.json to Azure Blob Container")

		// Verify that the OpenID provider issuer discovery document is publicly accessible.

		openIDConfigURL := path.Join(
			storageAccountURL,
			constants.WorkloadIdentityBlobContainerName,
			constants.AzureBlobNameOpenIDConfiguration,
		)

		response, err := http.Get(openIDConfigURL)
		assert.AssertErrNil(ctx, err, "Failed fetching uploaded openid-configuration.json")
		defer response.Body.Close()

		responseBody, err := io.ReadAll(response.Body)
		assert.AssertErrNil(ctx, err, "Failed reading fetched openid-configuration.json")

		assert.Assert(ctx,
			bytes.Equal(responseBody, openIDConfig),
			"Fetched openid-configuration.json, isn't as expected",
		)
	}

	{
		// Generate the JWKS document.
		utils.ExecuteCommandOrDie(fmt.Sprintf(
			"azwi jwks --public-keys %s --output-file %s",
			config.ParsedConfig.Cloud.Azure.WorkloadIdentitySSHPublicKeyFilePath,
			constants.OutputPathJWKSDocument,
		))

		jwksDocument, err := os.ReadFile(constants.OutputPathJWKSDocument)
		assert.AssertErrNil(ctx, err, "Failed reading the generated JWKS document")

		// Upload the JWKS document.
		_, err = blobClient.UploadBuffer(ctx,
			constants.WorkloadIdentityBlobContainerName,
			constants.AzureBlobNameJWKSDocument,
			jwksDocument,
			nil,
		)
		assert.AssertErrNil(ctx, err, "Failed uploading JWKS document to Azure Blob Container")

		// Verify that the JWKS document is publicly accessible.

		jwksDocumentConfigURL := path.Join(
			storageAccountURL,
			constants.WorkloadIdentityBlobContainerName,
			constants.AzureBlobNameJWKSDocument,
		)

		response, err := http.Get(jwksDocumentConfigURL)
		assert.AssertErrNil(ctx, err, "Failed fetching uploaded JWKS document")
		defer response.Body.Close()

		responseBody, err := io.ReadAll(response.Body)
		assert.AssertErrNil(ctx, err, "Failed reading fetched JWKS document")

		assert.Assert(ctx,
			bytes.Equal(responseBody, jwksDocument),
			"Fetched JWKS document, isn't as expected",
		)
	}
}
