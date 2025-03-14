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

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/authorization/armauthorization"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/msi/armmsi"
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

	// Create Azure Storage Account, if it doesn't already exist.

	storageClientFactory, err := armstorage.NewClientFactory(a.subscriptionID, a.credentials, nil)
	assert.AssertErrNil(ctx, err, "Failed creating Azure Storage client factory")

	storageAccountName := config.ParsedConfig.Cloud.Azure.WorkloadIdentity.StorageAccountName

	services.CreateStorageAccount(ctx, &services.CreateStorageAccountArgs{
		StorageAccountsClient: storageClientFactory.NewAccountsClient(),
		ResourceGroupName:     a.resourceGroupName,
		Name:                  storageAccountName,
	})

	// Create Azure Storage Container, if it doesn't already exist.
	services.CreateBlobContainer(ctx, &services.CreateBlobContainerArgs{
		ResourceGroupName:    a.resourceGroupName,
		StorageAccountName:   storageAccountName,
		BlobContainersClient: storageClientFactory.NewBlobContainersClient(),
		BlobContainerName:    constants.BlobContainerNameWorkloadIdentity,
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
				BlobContainerName:  constants.BlobContainerNameWorkloadIdentity,
			},
		)

		// Upload the OpenID provider issuer discovery document to the Azure Storage Container,
		// at path .well-known/openid-configuration.
		_, err := blobClient.UploadBuffer(ctx,
			constants.BlobContainerNameWorkloadIdentity,
			constants.AzureBlobNameOpenIDConfiguration,
			openIDConfig,
			nil,
		)
		assert.AssertErrNil(ctx, err, "Failed uploading openid-configuration.json to Azure Blob Container")

		// Verify that the OpenID provider issuer discovery document is publicly accessible.

		openIDConfigURL := path.Join(
			storageAccountURL,
			constants.BlobContainerNameWorkloadIdentity,
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

		slog.InfoContext(ctx, "Uploaded openid-configuration.json to Azure Blob Container")
	}

	{
		// Generate the JWKS document.
		utils.ExecuteCommandOrDie(fmt.Sprintf(
			"azwi jwks --public-keys %s --output-file %s",
			config.ParsedConfig.Cloud.Azure.WorkloadIdentity.SSHPublicKeyFilePath,
			constants.OutputPathJWKSDocument,
		))

		jwksDocument, err := os.ReadFile(constants.OutputPathJWKSDocument)
		assert.AssertErrNil(ctx, err, "Failed reading the generated JWKS document")

		// Upload the JWKS document.
		_, err = blobClient.UploadBuffer(ctx,
			constants.BlobContainerNameWorkloadIdentity,
			constants.AzureBlobNameJWKSDocument,
			jwksDocument,
			nil,
		)
		assert.AssertErrNil(ctx, err, "Failed uploading JWKS document to Azure Blob Container")

		// Verify that the JWKS document is publicly accessible.

		jwksDocumentConfigURL := path.Join(
			storageAccountURL,
			constants.BlobContainerNameWorkloadIdentity,
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

		slog.InfoContext(ctx, "Uploaded JWKS document to Azure Blob Container")
	}

	{
		userAssignedIdentitiesClient, err := armmsi.NewUserAssignedIdentitiesClient(a.subscriptionID, a.credentials, nil)
		assert.AssertErrNil(ctx, err, "Failed creating User Assigned Identities client")

		roleAssignmentsClient, err := armauthorization.NewRoleAssignmentsClient(a.subscriptionID, a.credentials, nil)
		assert.AssertErrNil(ctx, err, "Failed creating Role Assignments client")

		// Create a User Assigned Managed Identity and assign it the Contributor role scoped to the
		// subscription being used.
		services.CreateUserAssignedIdentity(ctx, services.CreateUserAssignedIdentityArgs{
			UserAssignedIdentitiesClient: userAssignedIdentitiesClient,
			RoleAssignmentsClient:        roleAssignmentsClient,
			ResourceGroupName:            a.resourceGroupName,
			Name:                         config.ParsedConfig.Cluster.Name,
		})

		/*
			Not all ServiceAccount tokens can be exchanged for a valid AAD (Azure Active Directory)
			token. A federated identity credential between an existing Kubernetes ServiceAccount and an
			AAD application or user-assigned managed identity has to be created in advance.
			REFERENCE : https://capz.sigs.k8s.io/topics/workload-identity#workload-identity.

			We need to create two federated identity credentials, one for CAPZ and one for ASO.
			You can view the steps for that here :
			https://azure.github.io/azure-workload-identity/docs/topics/federated-identity-credential.html.

			NOTE : CAPZ interfaces with Azure to create and manage some types of resources using Azure
			       Service Operator (ASO).
			       You can read about ASO here : https://azure.github.io/azure-service-operator/.
		*/

		// Create Federated Identity credential for Cluster API Provider Azure (CAPZ).
		utils.ExecuteCommandOrDie(fmt.Sprintf(
			`
        az identity federated-credential create \
          --name "" \
          --identity-name "%s" \
          --resource-group "%s" \
          --issuer "%s" \
          --subject "system:serviceaccount:%s:%s"
      `,
		))

		// Create Federated Identity credential for Azure Service Operator (ASO).
		utils.ExecuteCommandOrDie(fmt.Sprintf(
			`
        az identity federated-credential create \
          --name "" \
          --identity-name "%s" \
          --resource-group "%s" \
          --issuer "%s" \
          --subject "system:serviceaccount:%s:%s"
      `,
		))
	}

	slog.InfoContext(ctx, "Finished setting up the Workload Identity Provider")
}
