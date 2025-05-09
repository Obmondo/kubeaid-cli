package azure

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/authorization/armauthorization/v2"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/google/uuid"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud/azure/services"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/kubernetes"
	templateUtils "github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/templates"
)

//go:embed templates/*
var templates embed.FS

type TemplateArgs struct {
	StorageAccountName,
	BlobContainerName string
}

// Make sure you go through ./WorkloadIdentity.md first.
func (a *Azure) SetupWorkloadIdentityProvider(ctx context.Context) {
	slog.Info("Setting up Workload Identity provider....")

	azureConfig := config.ParsedGeneralConfig.Cloud.Azure

	// Do az login.
	// We'll use the Azure CLI to create Federated identity credentials.
	utils.ExecuteCommandOrDie(fmt.Sprintf(
		`
      az login \
        --service-principal \
        --username %s \
        --password %s \
        --tenant %s
    `,
		config.ParsedSecretsConfig.Azure.ClientID,
		config.ParsedSecretsConfig.Azure.ClientSecret,
		azureConfig.TenantID,
	))

	// Create the Service Account token issuer.
	serviceAccountIssuerURL := a.createExternalOpenIDProvider(ctx)

	{
		// Create a User Assigned Managed Identity, dedicated to ClusterAPI.
		// Assign it the Contributor role scoped to the subscription being used.
		_, globals.UAMIClientIDClusterAPI = services.CreateUAMI(ctx,
			services.CreateUAMIArgs{
				UAMIClient:            a.uamiClient,
				RoleAssignmentsClient: a.roleAssignmentsClient,

				SubscriptionID:    a.subscriptionID,
				ResourceGroupName: a.resourceGroupName,

				RoleID:              constants.AzureRoleIDContributor,
				RoleAssignmentScope: path.Join("/subscriptions/", a.subscriptionID),
				Name:                constants.UAMIClusterAPI,
			},
		)

		// Create Federated Identity credential for Cluster API Provider Azure (CAPZ).
		slog.InfoContext(ctx, "Creating Azure Federated Identity for CAPZ")
		utils.ExecuteCommandOrDie(fmt.Sprintf(
			`
        az identity federated-credential create \
          --name "capz-federated-identity" \
          --identity-name "%s" \
          --resource-group "%s" \
          --issuer "%s" \
          --subject "system:serviceaccount:%s:%s"
      `,
			constants.UAMIClusterAPI,
			a.resourceGroupName,
			serviceAccountIssuerURL,
			kubernetes.GetCapiClusterNamespace(),
			constants.ServiceAccountCAPZ,
		))

		/*
			Create Federated Identity credential for Azure Service Operator (ASO).

			NOTE : CAPZ interfaces with Azure to create and manage some types of resources using Azure
			       Service Operator (ASO).
			       You can read about ASO here : https://azure.github.io/azure-service-operator/.
		*/
		slog.InfoContext(ctx, "Creating Azure Federated Identity for Azure Service Operator (ASO)")
		utils.ExecuteCommandOrDie(fmt.Sprintf(
			`
        az identity federated-credential create \
          --name "aso-federated-identity" \
          --identity-name "%s" \
          --resource-group "%s" \
          --issuer "%s" \
          --subject "system:serviceaccount:%s:%s"
      `,
			constants.UAMIClusterAPI,
			a.resourceGroupName,
			serviceAccountIssuerURL,
			kubernetes.GetCapiClusterNamespace(),
			constants.ServiceAccountASO,
		))
	}

	if azureConfig.DisasterRecovery != nil {
		// Create a User Assigned Managed Identity (dedicated to Velero).
		// Assign it the Storage Data Owner role scoped to the Storage Account being used.
		_, globals.UAMIClientIDVelero = services.CreateUAMI(ctx,
			services.CreateUAMIArgs{
				UAMIClient:            a.uamiClient,
				RoleAssignmentsClient: a.roleAssignmentsClient,

				SubscriptionID:    a.subscriptionID,
				ResourceGroupName: a.resourceGroupName,

				RoleID:              constants.AzureRoleIDStorageBlobDataOwner,
				RoleAssignmentScope: path.Join("/subscriptions/", a.subscriptionID),
				Name:                constants.UAMIVelero,
			},
		)

		// Create Federated Identity credential for Velero.
		slog.InfoContext(ctx, "Creating Azure Federated Identity for Velero")
		utils.ExecuteCommandOrDie(fmt.Sprintf(
			`
        az identity federated-credential create \
          --name "velero-federated-identity" \
          --identity-name "%s" \
          --resource-group "%s" \
          --issuer "%s" \
          --subject "system:serviceaccount:%s:%s"
      `,
			constants.UAMIVelero,
			a.resourceGroupName,
			serviceAccountIssuerURL,
			constants.NamespaceVelero,
			constants.ServiceAccountVelero,
		))
	}

	slog.InfoContext(ctx, "Finished setting up the Workload Identity Provider")
}

func (a *Azure) createExternalOpenIDProvider(ctx context.Context) string {
	slog.InfoContext(ctx, "Setting up external OpenID provider...")

	azureConfig := config.ParsedGeneralConfig.Cloud.Azure

	// Create Azure Storage Account, if it doesn't already exist.

	storageAccountName := azureConfig.StorageAccount

	services.CreateStorageAccount(ctx, &services.CreateStorageAccountArgs{
		StorageAccountsClient: a.storageClientFactory.NewAccountsClient(),
		ResourceGroupName:     a.resourceGroupName,
		Name:                  storageAccountName,
	})

	// Allow the AAD app to access everything in this Azure Storage Container.
	{
		slog.InfoContext(ctx, "Assigning Storage Blob Data Owner role to AAD application")

		var (
			roleDefinitionID = fmt.Sprintf(
				"/subscriptions/%s/providers/Microsoft.Authorization/roleDefinitions/%s",
				a.subscriptionID,
				constants.AzureRoleIDStorageBlobDataOwner,
			)

			roleScope = fmt.Sprintf(
				"/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Storage/storageAccounts/%s",
				a.subscriptionID,
				a.resourceGroupName,
				storageAccountName,
			)

			roleAssignmentID = uuid.New().String()
		)

		_, err := a.roleAssignmentsClient.Create(ctx,
			roleScope,
			roleAssignmentID,
			armauthorization.RoleAssignmentCreateParameters{
				Properties: &armauthorization.RoleAssignmentProperties{
					PrincipalID:   to.Ptr(azureConfig.AADApplication.ServicePrincipalID),
					PrincipalType: to.Ptr(armauthorization.PrincipalTypeServicePrincipal),

					RoleDefinitionID: to.Ptr(roleDefinitionID),
				},
			},
			nil,
		)
		if err != nil {
			// Skip, if the Storage Account already exists.
			//nolint:errorlint
			responseError, ok := err.(*azcore.ResponseError)
			if ok &&
				responseError.StatusCode == constants.AzureResponseStatusCodeResourceAlreadyExists {
				slog.InfoContext(ctx,
					"Storage Blob Data Owner role is already assigned to AAD application",
				)
			} else {
				assert.AssertErrNil(ctx, err, "Failed assigning Storage Blob Data Owner role to AAD application")
			}
		}
	}

	// Create Azure Storage Container, if it doesn't already exist.
	services.CreateBlobContainer(ctx, &services.CreateBlobContainerArgs{
		ResourceGroupName:    a.resourceGroupName,
		StorageAccountName:   storageAccountName,
		BlobContainersClient: a.storageClientFactory.NewBlobContainersClient(),
		BlobContainerName:    constants.BlobContainerNameWorkloadIdentity,
	})

	storageAccountURL := GetStorageAccountURL()

	serviceAccountIssuerURL := GetServiceAccountIssuerURL(ctx)

	blobClient, err := azblob.NewClient(storageAccountURL, a.credentials, nil)
	assert.AssertErrNil(ctx, err, "Failed creating Azure Blob client")

	{
		slog.InfoContext(ctx, "Generating and uploading openid-configuration.json")

		// Generate the OpenID provider issuer discovery document.
		// You can read more about OpenID provider issuer discovery document here :
		// https://openid.net/specs/openid-connect-discovery-1_0.html.
		openIDConfig := templateUtils.ParseAndExecuteTemplate(ctx,
			&templates, constants.TemplateNameOpenIDConfig,
			&TemplateArgs{
				StorageAccountName: storageAccountName,
				BlobContainerName:  constants.BlobContainerNameWorkloadIdentity,
			},
		)

		/*
			Upload the OpenID provider issuer discovery document to the Azure Storage Container,
			at path .well-known/openid-configuration.

			NOTE : We need to retry, since this fails until around a minute has passed after the creation
			       of the Azure Blob Container.
		*/
		err = utils.WithRetry(10*time.Second, 6, func() error {
			_, err := blobClient.UploadBuffer(ctx,
				constants.BlobContainerNameWorkloadIdentity,
				constants.AzureBlobNameOpenIDConfiguration,
				openIDConfig,
				nil,
			)
			return err
		})
		assert.AssertErrNil(ctx, err,
			"Failed uploading openid-configuration.json to Azure Blob Container",
		)

		// Verify that the OpenID provider issuer discovery document is publicly accessible.

		openIDConfigURL, err := url.JoinPath(
			serviceAccountIssuerURL,
			constants.AzureBlobNameOpenIDConfiguration,
		)
		assert.AssertErrNil(ctx, err, "Failed constructing OpenID config URL path")

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
		slog.InfoContext(ctx, "Generating and uploading JWKS document")

		// Generate the JWKS document.
		utils.ExecuteCommandOrDie(fmt.Sprintf(
			"azwi jwks --public-keys %s --output-file %s",
			azureConfig.WorkloadIdentity.OpenIDProviderSSHKeyPair.PublicKeyFilePath,
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

		jwksDocumentConfigURL, err := url.JoinPath(
			serviceAccountIssuerURL,
			constants.AzureBlobNameJWKSDocument,
		)
		assert.AssertErrNil(ctx, err, "Failed constructing OpenID config URL path")

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

	slog.InfoContext(ctx, "Finished setting up external OpenID provider")

	return serviceAccountIssuerURL
}
