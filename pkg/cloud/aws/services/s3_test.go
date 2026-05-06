// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package services

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3Types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud/aws/services/fake"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils"
)

// Mutates config.ParsedGeneralConfig — sequential only.
func TestCreateS3Bucket(t *testing.T) {
	tests := []struct {
		name    string
		region  string
		client  *fake.S3API
		wantErr bool
		errMsg  string
	}{
		{
			name:   "bucket created successfully in us-east-1",
			region: "us-east-1",
			client: &fake.S3API{
				CreateBucketOutput: &s3.CreateBucketOutput{},
				HeadBucketOutput:   &s3.HeadBucketOutput{},
			},
		},
		{
			name:   "bucket created successfully in non-us-east-1 region",
			region: "eu-west-1",
			client: &fake.S3API{
				CreateBucketOutput: &s3.CreateBucketOutput{},
				HeadBucketOutput:   &s3.HeadBucketOutput{},
			},
		},
		{
			name:   "bucket already owned by you returns nil",
			region: "us-east-1",
			client: &fake.S3API{
				CreateBucketErr: &s3Types.BucketAlreadyOwnedByYou{},
			},
		},
		{
			name:   "CreateBucket returns other error",
			region: "us-east-1",
			client: &fake.S3API{
				CreateBucketErr: fmt.Errorf("access denied"),
			},
			wantErr: true,
			errMsg:  "creating S3 bucket",
		},
		{
			name:   "waiter fails after bucket creation",
			region: "us-east-1",
			client: &fake.S3API{
				CreateBucketOutput: &s3.CreateBucketOutput{},
				HeadBucketErr:      fmt.Errorf("not found"),
			},
			wantErr: true,
			errMsg:  "waiting for S3 bucket",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			savedCfg := config.ParsedGeneralConfig
			t.Cleanup(func() { config.ParsedGeneralConfig = savedCfg })

			config.ParsedGeneralConfig = &config.GeneralConfig{
				Cloud: config.CloudConfig{
					AWS: &config.AWSConfig{Region: tc.region},
				},
			}

			err := CreateS3Bucket(context.Background(), tc.client, "test-bucket")
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errMsg)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestDownloadS3BucketContents(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		bucketName string
		gzipDecode bool
		setupFake  func(t *testing.T) *fake.S3API
		wantErr    bool
		errMsg     string
		validate   func(t *testing.T, downloadDir string)
	}{
		{
			name:       "downloads single object without gzip",
			bucketName: "bucket-plain",
			gzipDecode: false,
			setupFake: func(t *testing.T) *fake.S3API {
				t.Helper()
				return &fake.S3API{
					ListObjectsOutputs: []*s3.ListObjectsV2Output{
						{
							Contents: []s3Types.Object{
								{Key: aws.String("file1.txt")},
							},
							IsTruncated: aws.Bool(false),
						},
					},
					GetObjectOutputs: map[string]*s3.GetObjectOutput{
						"file1.txt": {
							Body: io.NopCloser(bytes.NewReader([]byte("hello world"))),
						},
					},
				}
			},
			validate: func(t *testing.T, downloadDir string) {
				t.Helper()
				content, err := os.ReadFile(filepath.Join(downloadDir, "file1.txt"))
				require.NoError(t, err)
				assert.Equal(t, "hello world", string(content))
			},
		},
		{
			name:       "downloads gzipped object",
			bucketName: "bucket-gzip",
			gzipDecode: true,
			setupFake: func(t *testing.T) *fake.S3API {
				t.Helper()
				var buf bytes.Buffer
				gw := gzip.NewWriter(&buf)
				_, err := gw.Write([]byte("decompressed content"))
				require.NoError(t, err)
				require.NoError(t, gw.Close())

				return &fake.S3API{
					ListObjectsOutputs: []*s3.ListObjectsV2Output{
						{
							Contents: []s3Types.Object{
								{Key: aws.String("file1.gz")},
							},
							IsTruncated: aws.Bool(false),
						},
					},
					GetObjectOutputs: map[string]*s3.GetObjectOutput{
						"file1.gz": {
							Body: io.NopCloser(bytes.NewReader(buf.Bytes())),
						},
					},
				}
			},
			validate: func(t *testing.T, downloadDir string) {
				t.Helper()
				content, err := os.ReadFile(filepath.Join(downloadDir, "file1.gz"))
				require.NoError(t, err)
				assert.Equal(t, "decompressed content", string(content))
			},
		},
		{
			name:       "handles paginated results",
			bucketName: "bucket-paginated",
			gzipDecode: false,
			setupFake: func(t *testing.T) *fake.S3API {
				t.Helper()
				token := "page2token"
				return &fake.S3API{
					ListObjectsOutputs: []*s3.ListObjectsV2Output{
						{
							Contents: []s3Types.Object{
								{Key: aws.String("page1.txt")},
							},
							IsTruncated:       aws.Bool(true),
							ContinuationToken: &token,
						},
						{
							Contents: []s3Types.Object{
								{Key: aws.String("page2.txt")},
							},
							IsTruncated: aws.Bool(false),
						},
					},
					GetObjectOutputs: map[string]*s3.GetObjectOutput{
						"page1.txt": {
							Body: io.NopCloser(bytes.NewReader([]byte("page1"))),
						},
						"page2.txt": {
							Body: io.NopCloser(bytes.NewReader([]byte("page2"))),
						},
					},
				}
			},
			validate: func(t *testing.T, downloadDir string) {
				t.Helper()
				content1, err := os.ReadFile(filepath.Join(downloadDir, "page1.txt"))
				require.NoError(t, err)
				assert.Equal(t, "page1", string(content1))

				content2, err := os.ReadFile(filepath.Join(downloadDir, "page2.txt"))
				require.NoError(t, err)
				assert.Equal(t, "page2", string(content2))
			},
		},
		{
			name:       "ListObjectsV2 returns error",
			bucketName: "bucket-listerr",
			gzipDecode: false,
			setupFake: func(_ *testing.T) *fake.S3API {
				return &fake.S3API{
					ListObjectsErr: fmt.Errorf("access denied"),
				}
			},
			wantErr: true,
			errMsg:  "listing objects in S3 bucket",
		},
		{
			name:       "path traversal in object key is rejected",
			bucketName: "bucket-traversal",
			gzipDecode: false,
			setupFake: func(_ *testing.T) *fake.S3API {
				return &fake.S3API{
					ListObjectsOutputs: []*s3.ListObjectsV2Output{
						{
							Contents: []s3Types.Object{
								{Key: aws.String("../../etc/passwd")},
							},
							IsTruncated: aws.Bool(false),
						},
					},
				}
			},
			wantErr: true,
			errMsg:  "escapes download directory",
		},
		{
			name:       "GetObject returns error",
			bucketName: "bucket-geterr",
			gzipDecode: false,
			setupFake: func(_ *testing.T) *fake.S3API {
				return &fake.S3API{
					ListObjectsOutputs: []*s3.ListObjectsV2Output{
						{
							Contents: []s3Types.Object{
								{Key: aws.String("file1.txt")},
							},
							IsTruncated: aws.Bool(false),
						},
					},
					GetObjectErr: fmt.Errorf("no such key"),
				}
			},
			wantErr: true,
			errMsg:  "downloading S3 object",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// DownloadS3BucketContents uses utils.GetDownloadedStorageBucketContentsDir
			// which resolves to a hardcoded path under /tmp; cannot redirect to t.TempDir().
			downloadDir := utils.GetDownloadedStorageBucketContentsDir(tc.bucketName)
			t.Cleanup(func() { _ = os.RemoveAll(downloadDir) })

			fake := tc.setupFake(t)

			err := DownloadS3BucketContents(context.Background(), fake, tc.bucketName, tc.gzipDecode)
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errMsg)
				return
			}
			require.NoError(t, err)

			if tc.validate != nil {
				tc.validate(t, downloadDir)
			}
		})
	}
}
