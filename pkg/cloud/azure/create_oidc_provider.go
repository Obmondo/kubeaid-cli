package azure

import (
	"bytes"
	"context"
	"embed"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-workload-identity/pkg/cmd/jwks"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	templateUtils "github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/templates"
)

//go:embed templates/*
var templates embed.FS

type TemplateArgs struct {
	StorageAccountName,
	BlobContainerName string
}

func (a *Azure) CreateOIDCProvider(ctx context.Context) {
	slog.InfoContext(ctx, "Setting up OIDC provider...")

	var (
		azureConfig = config.ParsedGeneralConfig.Cloud.Azure

		storageAccountName = azureConfig.StorageAccount
		storageAccountURL  = GetStorageAccountURL()

		serviceAccountIssuerURL = GetServiceAccountIssuerURL(ctx)
	)

	blobClient, err := azblob.NewClient(storageAccountURL, a.credentials, nil)
	assert.AssertErrNil(ctx, err, "Failed creating Azure Blob client")

	// Generate and upload the OIDC provider discovery document.
	{
		slog.InfoContext(ctx, "Generating and uploading openid-configuration.json")

		// Generate the OIDC provider discovery document.
		// You can read more about OIDC provider discovery document here :
		// https://openid.net/specs/openid-connect-discovery-1_0.html.
		openIDConfig := templateUtils.ParseAndExecuteTemplate(ctx,
			&templates, constants.TemplateNameOpenIDConfig,
			&TemplateArgs{
				StorageAccountName: storageAccountName,
				BlobContainerName:  constants.BlobContainerNameOIDCProvider,
			},
		)

		/*
			Upload the OIDC provider discovery document to the Azure Storage Container,
			at path .well-known/openid-configuration.

			NOTE : We need to retry, since this fails until around a minute has passed after the creation
			       of the Azure Blob Container.
		*/
		err = utils.WithRetry(10*time.Second, 6, func() error {
			_, err := blobClient.UploadBuffer(ctx,
				constants.BlobContainerNameOIDCProvider,
				constants.AzureBlobNameOpenIDConfiguration,
				openIDConfig,
				nil,
			)
			return err
		})
		assert.AssertErrNil(ctx, err,
			"Failed uploading openid-configuration.json to Azure Blob Container",
		)

		// Verify that the OIDC provider discovery document is publicly accessible.

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

	// Generate and upload the JWKS document.
	{
		slog.InfoContext(ctx, "Generating and uploading JWKS document")

		// Create required intermediate directories,
		// to save the JWKS document locally.
		utils.CreateIntermediateDirsForFile(ctx, constants.OutputPathJWKSDocument)

		// Generate the JWKS document.

		jwksCmd := jwks.NewJWKSCmd()
		jwksCmd.SetArgs([]string{
			"--public-keys",
			azureConfig.WorkloadIdentity.OpenIDProviderSSHKeyPair.PublicKeyFilePath,

			"--output-file",
			constants.OutputPathJWKSDocument,
		})
		err := jwksCmd.ExecuteContext(ctx)
		assert.AssertErrNil(ctx, err, "Failed generating JWKS document")

		jwksDocument, err := os.ReadFile(constants.OutputPathJWKSDocument)
		assert.AssertErrNil(ctx, err, "Failed reading the generated JWKS document")

		// Upload the JWKS document.
		_, err = blobClient.UploadBuffer(ctx,
			constants.BlobContainerNameOIDCProvider,
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

	slog.InfoContext(ctx, "Finished setting up OIDC provider")
}
