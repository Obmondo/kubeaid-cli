// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package azure

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils"
)

// fakeBlobDownloader implements blobDownloader for unit tests.
type fakeBlobDownloader struct {
	// blobs maps blobName to content bytes to serve.
	blobs map[string][]byte
	// err is returned from DownloadStream when non-nil.
	err error
}

func (f *fakeBlobDownloader) DownloadStream(
	_ context.Context,
	_ string,
	blobName string,
	_ *azblob.DownloadStreamOptions,
) (azblob.DownloadStreamResponse, error) {
	if f.err != nil {
		return azblob.DownloadStreamResponse{}, f.err
	}
	data, ok := f.blobs[blobName]
	if !ok {
		return azblob.DownloadStreamResponse{}, fmt.Errorf("blob %q not found in fake", blobName)
	}
	return azblob.DownloadStreamResponse{
		DownloadResponse: blob.DownloadResponse{
			Body: io.NopCloser(bytes.NewReader(data)),
		},
	}, nil
}

func TestDownloadBlobContent(t *testing.T) {
	t.Parallel()

	gzipBytes := func(t *testing.T, data []byte) []byte {
		t.Helper()
		var buf bytes.Buffer
		w := gzip.NewWriter(&buf)
		_, err := w.Write(data)
		require.NoError(t, err)
		require.NoError(t, w.Close())
		return buf.Bytes()
	}

	tests := []struct {
		name        string
		blobName    string
		blobData    map[string][]byte
		dlErr       error
		wantFile    string
		wantContent string
		wantErr     bool
		errContains string
	}{
		{
			name:     "plain file download",
			blobName: "backup.yaml",
			blobData: map[string][]byte{
				"backup.yaml": []byte("apiVersion: v1\nkind: Secret\n"),
			},
			wantFile:    "backup.yaml",
			wantContent: "apiVersion: v1\nkind: Secret\n",
		},
		{
			name:     "file without extension gets .yaml suffix",
			blobName: "sealed-secret-no-ext",
			blobData: map[string][]byte{
				"sealed-secret-no-ext": []byte("content-without-ext"),
			},
			wantFile:    "sealed-secret-no-ext.yaml",
			wantContent: "content-without-ext",
		},
		{
			name:     "nested path creates intermediate directories",
			blobName: "subdir/nested/file.yaml",
			blobData: map[string][]byte{
				"subdir/nested/file.yaml": []byte("nested-content"),
			},
			wantFile:    "subdir/nested/file.yaml",
			wantContent: "nested-content",
		},
		{
			name:     "path traversal in blob name is rejected",
			blobName: "../../etc/passwd",
			blobData: map[string][]byte{
				"../../etc/passwd": []byte("malicious"),
			},
			wantErr:     true,
			errContains: "escapes download directory",
		},
		{
			name:        "DownloadStream returns error",
			blobName:    "fail.yaml",
			dlErr:       fmt.Errorf("network timeout"),
			wantErr:     true,
			errContains: "downloading blob content",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			downloadDir := t.TempDir()
			fake := &fakeBlobDownloader{
				blobs: tc.blobData,
				err:   tc.dlErr,
			}

			err := downloadBlobContent(context.Background(), &downloadBlobContentArgs{
				BlobClient:        fake,
				DownloadDir:       downloadDir,
				BlobContainerName: "test-container",
				BlobName:          tc.blobName,
			})

			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errContains)
				return
			}
			require.NoError(t, err)

			got, err := os.ReadFile(filepath.Join(downloadDir, tc.wantFile))
			require.NoError(t, err)
			assert.Equal(t, tc.wantContent, string(got))
		})
	}

	t.Run("gzipped file gets decompressed and suffix stripped", func(t *testing.T) {
		t.Parallel()

		downloadDir := t.TempDir()
		plainContent := "gzip-decoded-content"
		compressed := gzipBytes(t, []byte(plainContent))

		fake := &fakeBlobDownloader{
			blobs: map[string][]byte{
				"archive.yaml.gz": compressed,
			},
		}

		err := downloadBlobContent(context.Background(), &downloadBlobContentArgs{
			BlobClient:        fake,
			DownloadDir:       downloadDir,
			BlobContainerName: "test-container",
			BlobName:          "archive.yaml.gz",
		})
		require.NoError(t, err)

		got, err := os.ReadFile(filepath.Join(downloadDir, "archive.yaml"))
		require.NoError(t, err)
		assert.Equal(t, plainContent, string(got))
	})
}

// Mutates listBlobNamesFn — sequential only.
func TestDownloadBlobContainerContents(t *testing.T) {
	tests := []struct {
		name        string
		listFn      func(ctx context.Context, client *azblob.Client, containerName string) ([]string, error)
		wantErr     bool
		errContains string
	}{
		{
			name: "empty blob list succeeds with no downloads",
			listFn: func(_ context.Context, _ *azblob.Client, _ string) ([]string, error) {
				return []string{}, nil
			},
		},
		{
			name: "listBlobNamesFn returns error",
			listFn: func(_ context.Context, _ *azblob.Client, _ string) ([]string, error) {
				return nil, fmt.Errorf("storage unavailable")
			},
			wantErr:     true,
			errContains: "listing blobs",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			savedListFn := listBlobNamesFn
			t.Cleanup(func() { listBlobNamesFn = savedListFn })

			listBlobNamesFn = tc.listFn

			// DownloadBlobContainerContents writes to /tmp/kubeaid-core/buckets/<containerName>.
			// We cannot redirect that without adding a seam for the download directory.
			// The success path with actual blob downloads requires a real *azblob.Client
			// because the concrete type is passed through; see TestDownloadBlobContent for
			// coverage of the actual download logic via the blobDownloader interface.
			containerName := "test-dl-" + tc.name
			t.Cleanup(func() { _ = os.RemoveAll(utils.GetDownloadedStorageBucketContentsDir(containerName)) })
			err := DownloadBlobContainerContents(context.Background(), nil, containerName)

			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errContains)
				return
			}
			require.NoError(t, err)
		})
	}
}
