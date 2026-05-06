// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package azure

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
)

// Mutates newBlobClientFn, uploadBlobBufferFn, httpGetFn, generateJWKSDocumentFn,
// config.ParsedGeneralConfig — sequential only.
func TestCreateOIDCProvider(t *testing.T) {
	validConfig := &config.GeneralConfig{
		Cloud: config.CloudConfig{
			Azure: &config.AzureConfig{
				StorageAccount: "teststorage",
				WorkloadIdentity: config.WorkloadIdentity{
					OpenIDProviderSSHKeyPair: config.OpenIDProviderSSHKeyPairConfig{
						PublicKeyFilePath: "/tmp/fake-key.pub",
					},
				},
			},
		},
	}

	expectedOpenIDConfigBytes, err := os.ReadFile("../../../testdata/azure/openid-configuration.json")
	require.NoError(t, err)
	expectedOpenIDConfig := string(expectedOpenIDConfigBytes)

	jwksContent := []byte(`{"keys":[]}`)

	tests := []struct {
		name             string
		slow             bool
		newBlobClient    func(string, *azidentity.ClientSecretCredential) (*azblob.Client, error)
		uploadBlobBuffer func(ctx context.Context, client *azblob.Client, containerName, blobName string, data []byte) error
		httpGet          func(url string) (*http.Response, error)
		generateJWKS     func(ctx context.Context, publicKeyPath, outputPath string) error
		wantErr          bool
		errMsg           string
	}{
		{
			name: "newBlobClientFn fails",
			newBlobClient: func(_ string, _ *azidentity.ClientSecretCredential) (*azblob.Client, error) {
				return nil, fmt.Errorf("bad credentials")
			},
			wantErr: true,
			errMsg:  "creating Azure Blob client",
		},
		{
			name: "upload openid-configuration fails",
			slow: true,
			newBlobClient: func(_ string, _ *azidentity.ClientSecretCredential) (*azblob.Client, error) {
				return nil, nil
			},
			uploadBlobBuffer: func(_ context.Context, _ *azblob.Client, _, blobName string, _ []byte) error {
				if blobName == constants.AzureBlobNameOpenIDConfiguration {
					return fmt.Errorf("upload denied")
				}
				return nil
			},
			wantErr: true,
			errMsg:  "uploading openid-configuration.json to Azure Blob Container",
		},
		{
			name: "httpGet for openid-configuration fails",
			newBlobClient: func(_ string, _ *azidentity.ClientSecretCredential) (*azblob.Client, error) {
				return nil, nil
			},
			uploadBlobBuffer: func(_ context.Context, _ *azblob.Client, _, _ string, _ []byte) error {
				return nil
			},
			httpGet: func(_ string) (*http.Response, error) {
				return nil, fmt.Errorf("network unreachable")
			},
			wantErr: true,
			errMsg:  "fetching uploaded openid-configuration.json",
		},
		{
			name: "openid-configuration content mismatch",
			newBlobClient: func(_ string, _ *azidentity.ClientSecretCredential) (*azblob.Client, error) {
				return nil, nil
			},
			uploadBlobBuffer: func(_ context.Context, _ *azblob.Client, _, _ string, _ []byte) error {
				return nil
			},
			httpGet: func(_ string) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader("wrong content")),
				}, nil
			},
			wantErr: true,
			errMsg:  "fetched openid-configuration.json does not match expected content",
		},
		{
			name: "generateJWKSDocumentFn fails",
			newBlobClient: func(_ string, _ *azidentity.ClientSecretCredential) (*azblob.Client, error) {
				return nil, nil
			},
			uploadBlobBuffer: func(_ context.Context, _ *azblob.Client, _, _ string, _ []byte) error {
				return nil
			},
			httpGet: func(reqURL string) (*http.Response, error) {
				if strings.Contains(reqURL, "openid-configuration") {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader(expectedOpenIDConfig)),
					}, nil
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(string(jwksContent))),
				}, nil
			},
			generateJWKS: func(_ context.Context, _, _ string) error {
				return fmt.Errorf("key generation failed")
			},
			wantErr: true,
			errMsg:  "generating JWKS document",
		},
		{
			name: "upload JWKS document fails",
			newBlobClient: func(_ string, _ *azidentity.ClientSecretCredential) (*azblob.Client, error) {
				return nil, nil
			},
			uploadBlobBuffer: func(_ context.Context, _ *azblob.Client, _, blobName string, _ []byte) error {
				if blobName == constants.AzureBlobNameJWKSDocument {
					return fmt.Errorf("upload JWKS denied")
				}
				return nil
			},
			httpGet: func(reqURL string) (*http.Response, error) {
				if strings.Contains(reqURL, "openid-configuration") {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader(expectedOpenIDConfig)),
					}, nil
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(string(jwksContent))),
				}, nil
			},
			generateJWKS: func(_ context.Context, _, outputPath string) error {
				return os.WriteFile(outputPath, jwksContent, 0o600)
			},
			wantErr: true,
			errMsg:  "uploading JWKS document to Azure Blob Container",
		},
		{
			name: "JWKS document content mismatch",
			newBlobClient: func(_ string, _ *azidentity.ClientSecretCredential) (*azblob.Client, error) {
				return nil, nil
			},
			uploadBlobBuffer: func(_ context.Context, _ *azblob.Client, _, _ string, _ []byte) error {
				return nil
			},
			httpGet: func(reqURL string) (*http.Response, error) {
				if strings.Contains(reqURL, "openid-configuration") {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader(expectedOpenIDConfig)),
					}, nil
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader("wrong jwks")),
				}, nil
			},
			generateJWKS: func(_ context.Context, _, outputPath string) error {
				return os.WriteFile(outputPath, jwksContent, 0o600)
			},
			wantErr: true,
			errMsg:  "fetched JWKS document does not match expected content",
		},
		{
			name: "full success",
			newBlobClient: func(_ string, _ *azidentity.ClientSecretCredential) (*azblob.Client, error) {
				return nil, nil
			},
			uploadBlobBuffer: func(_ context.Context, _ *azblob.Client, _, _ string, _ []byte) error {
				return nil
			},
			httpGet: func(reqURL string) (*http.Response, error) {
				if strings.Contains(reqURL, "openid-configuration") {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader(expectedOpenIDConfig)),
					}, nil
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(string(jwksContent))),
				}, nil
			},
			generateJWKS: func(_ context.Context, _, outputPath string) error {
				return os.WriteFile(outputPath, jwksContent, 0o600)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.slow && testing.Short() {
				t.Skip("skipping slow test (WithRetry sleeps 50s)")
			}

			savedConfig := config.ParsedGeneralConfig
			savedBlobClient := newBlobClientFn
			savedUpload := uploadBlobBufferFn
			savedHTTPGet := httpGetFn
			savedGenerateJWKS := generateJWKSDocumentFn
			t.Cleanup(func() {
				config.ParsedGeneralConfig = savedConfig
				newBlobClientFn = savedBlobClient
				uploadBlobBufferFn = savedUpload
				httpGetFn = savedHTTPGet
				generateJWKSDocumentFn = savedGenerateJWKS
				// constants.OutputPathJWKSDocument is a hardcoded relative path ("outputs/..."),
				// so we cannot redirect to t.TempDir() without adding a production seam.
				_ = os.RemoveAll("outputs")
			})

			config.ParsedGeneralConfig = validConfig
			newBlobClientFn = tc.newBlobClient
			if tc.uploadBlobBuffer != nil {
				uploadBlobBufferFn = tc.uploadBlobBuffer
			}
			if tc.httpGet != nil {
				httpGetFn = tc.httpGet
			}
			if tc.generateJWKS != nil {
				generateJWKSDocumentFn = tc.generateJWKS
			}

			a := &Azure{}
			err := a.CreateOIDCProvider(context.Background())

			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errMsg)
				return
			}
			require.NoError(t, err)
		})
	}
}
