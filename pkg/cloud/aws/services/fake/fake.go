// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package fake

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3API is a configurable fake implementation of services.S3API for testing.
type S3API struct {
	CreateBucketOutput *s3.CreateBucketOutput
	CreateBucketErr    error

	HeadBucketOutput *s3.HeadBucketOutput
	HeadBucketErr    error

	ListObjectsOutputs []*s3.ListObjectsV2Output
	ListObjectsErr     error
	listObjectsCall    int

	GetObjectOutputs map[string]*s3.GetObjectOutput
	GetObjectErr     error
}

func (f *S3API) CreateBucket(_ context.Context, _ *s3.CreateBucketInput, _ ...func(*s3.Options)) (*s3.CreateBucketOutput, error) {
	return f.CreateBucketOutput, f.CreateBucketErr
}

func (f *S3API) HeadBucket(_ context.Context, _ *s3.HeadBucketInput, _ ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
	return f.HeadBucketOutput, f.HeadBucketErr
}

func (f *S3API) ListObjectsV2(_ context.Context, _ *s3.ListObjectsV2Input, _ ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	if f.ListObjectsErr != nil {
		return nil, f.ListObjectsErr
	}
	if f.listObjectsCall >= len(f.ListObjectsOutputs) {
		return nil, fmt.Errorf("unexpected ListObjectsV2 call")
	}
	out := f.ListObjectsOutputs[f.listObjectsCall]
	f.listObjectsCall++
	return out, nil
}

func (f *S3API) GetObject(_ context.Context, input *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	if f.GetObjectErr != nil {
		return nil, f.GetObjectErr
	}
	out, ok := f.GetObjectOutputs[*input.Key]
	if !ok {
		return nil, fmt.Errorf("key %s not found", *input.Key)
	}
	return out, nil
}

// IAMAPI is a configurable fake implementation of services.IAMAPI for testing.
type IAMAPI struct {
	CreatePolicyErr     error
	CreateRoleErr       error
	AttachRolePolicyErr error
}

func (f *IAMAPI) CreatePolicy(_ context.Context, _ *iam.CreatePolicyInput, _ ...func(*iam.Options)) (*iam.CreatePolicyOutput, error) {
	return &iam.CreatePolicyOutput{}, f.CreatePolicyErr
}

func (f *IAMAPI) CreateRole(_ context.Context, _ *iam.CreateRoleInput, _ ...func(*iam.Options)) (*iam.CreateRoleOutput, error) {
	return &iam.CreateRoleOutput{}, f.CreateRoleErr
}

func (f *IAMAPI) AttachRolePolicy(_ context.Context, _ *iam.AttachRolePolicyInput, _ ...func(*iam.Options)) (*iam.AttachRolePolicyOutput, error) {
	return &iam.AttachRolePolicyOutput{}, f.AttachRolePolicyErr
}
