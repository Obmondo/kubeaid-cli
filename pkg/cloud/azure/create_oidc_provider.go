// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

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
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-workload-identity/pkg/cmd/jwks"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/utils"
	templateUtils "github.com/Obmondo/kubeaid-cli/pkg/utils/templates"
)

//go:embed templates/*
var templates embed.FS

type TemplateArgs struct {
	StorageAccountName,
	BlobContainerName string
}

var httpGetFn = http.Get

var newBlobClientFn = func(storageAccountURL string, cred *azidentity.ClientSecretCredential) (*azblob.Client, error) {
	return azblob.NewClient(storageAccountURL, cred, nil)
}

var uploadBlobBufferFn = func(ctx context.Context, client *azblob.Client, containerName, blobName string, data []byte) error {
	_, err := client.UploadBuffer(ctx, containerName, blobName, data, nil)
	return err
}

var generateJWKSDocumentFn = func(ctx context.Context, publicKeyPath, outputPath string) error {
	jwksCmd := jwks.NewJWKSCmd()
	jwksCmd.SetArgs([]string{
		"--public-keys", publicKeyPath,
		"--output-file", outputPath,
	})
	return jwksCmd.ExecuteContext(ctx)
}

func (a *Azure) CreateOIDCProvider(ctx context.Context) error {
	slog.InfoContext(ctx, "Setting up OIDC provider...")

	var (
		azureConfig = config.ParsedGeneralConfig.Cloud.Azure

		storageAccountName = azureConfig.StorageAccount
		storageAccountURL  = GetStorageAccountURL()
	)

	serviceAccountIssuerURL, err := GetServiceAccountIssuerURL()
	if err != nil {
		return fmt.Errorf("getting service account issuer URL: %w", err)
	}

	blobClient, err := newBlobClientFn(storageAccountURL, a.credentials)
	if err != nil {
		return fmt.Errorf("creating Azure Blob client: %w", err)
	}

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
			return uploadBlobBufferFn(ctx, blobClient,
				constants.BlobContainerNameOIDCProvider,
				constants.AzureBlobNameOpenIDConfiguration,
				openIDConfig,
			)
		})
		if err != nil {
			return fmt.Errorf("uploading openid-configuration.json to Azure Blob Container: %w", err)
		}

		// Verify that the OIDC provider discovery document is publicly accessible.

		openIDConfigURL, err := url.JoinPath(
			serviceAccountIssuerURL,
			constants.AzureBlobNameOpenIDConfiguration,
		)
		if err != nil {
			return fmt.Errorf("constructing OpenID config URL path: %w", err)
		}

		response, err := httpGetFn(openIDConfigURL) //nolint:gosec
		if err != nil {
			return fmt.Errorf("fetching uploaded openid-configuration.json: %w", err)
		}
		defer response.Body.Close()

		responseBody, err := io.ReadAll(response.Body)
		if err != nil {
			return fmt.Errorf("reading fetched openid-configuration.json: %w", err)
		}

		if !bytes.Equal(responseBody, openIDConfig) {
			return fmt.Errorf("fetched openid-configuration.json does not match expected content")
		}
	}

	// Generate and upload the JWKS document.
	{
		slog.InfoContext(ctx, "Generating and uploading JWKS document")

		// Create required intermediate directories,
		// to save the JWKS document locally.
		err := utils.CreateIntermediateDirsForFile(constants.OutputPathJWKSDocument)
		if err != nil {
			return fmt.Errorf("creating intermediate dirs for JWKS document: %w", err)
		}

		// Generate the JWKS document.
		err = generateJWKSDocumentFn(ctx,
			azureConfig.WorkloadIdentity.OpenIDProviderSSHKeyPair.PublicKeyFilePath,
			constants.OutputPathJWKSDocument,
		)
		if err != nil {
			return fmt.Errorf("generating JWKS document: %w", err)
		}

		jwksDocument, err := os.ReadFile(constants.OutputPathJWKSDocument)
		if err != nil {
			return fmt.Errorf("reading the generated JWKS document: %w", err)
		}

		// Upload the JWKS document.
		err = uploadBlobBufferFn(ctx, blobClient,
			constants.BlobContainerNameOIDCProvider,
			constants.AzureBlobNameJWKSDocument,
			jwksDocument,
		)
		if err != nil {
			return fmt.Errorf("uploading JWKS document to Azure Blob Container: %w", err)
		}

		// Verify that the JWKS document is publicly accessible.

		jwksDocumentConfigURL, err := url.JoinPath(
			serviceAccountIssuerURL,
			constants.AzureBlobNameJWKSDocument,
		)
		if err != nil {
			return fmt.Errorf("constructing JWKS document URL path: %w", err)
		}

		response, err := httpGetFn(jwksDocumentConfigURL) //nolint:gosec
		if err != nil {
			return fmt.Errorf("fetching uploaded JWKS document: %w", err)
		}
		defer response.Body.Close()

		responseBody, err := io.ReadAll(response.Body)
		if err != nil {
			return fmt.Errorf("reading fetched JWKS document: %w", err)
		}

		if !bytes.Equal(responseBody, jwksDocument) {
			return fmt.Errorf("fetched JWKS document does not match expected content")
		}
	}

	slog.InfoContext(ctx, "Finished setting up OIDC provider")
	return nil
}
