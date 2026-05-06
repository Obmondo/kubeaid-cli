// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package aws

import (
	"context"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud"
)

type fakeEC2Client struct {
	output *ec2.DescribeInstanceTypesOutput
	err    error
}

func (f *fakeEC2Client) DescribeInstanceTypes(
	_ context.Context,
	_ *ec2.DescribeInstanceTypesInput,
	_ ...func(*ec2.Options),
) (*ec2.DescribeInstanceTypesOutput, error) {
	return f.output, f.err
}

func TestGetVMSpecs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		vmType    string
		ec2Client *fakeEC2Client
		want      *cloud.VMSpec
		wantErr   bool
		errMsg    string
	}{
		{
			name:   "returns correct CPU and memory",
			vmType: "m5.large",
			ec2Client: &fakeEC2Client{
				output: &ec2.DescribeInstanceTypesOutput{
					InstanceTypes: []types.InstanceTypeInfo{
						{
							VCpuInfo: &types.VCpuInfo{
								DefaultVCpus: aws.Int32(2),
							},
							MemoryInfo: &types.MemoryInfo{
								SizeInMiB: aws.Int64(8192),
							},
						},
					},
				},
			},
			want: &cloud.VMSpec{
				CPU:    2,
				Memory: 8,
			},
		},
		{
			name:   "EC2 API returns error",
			vmType: "invalid.type",
			ec2Client: &fakeEC2Client{
				err: fmt.Errorf("throttled"),
			},
			wantErr: true,
			errMsg:  "describing EC2 instance type",
		},
		{
			name:   "empty InstanceTypes returns error",
			vmType: "m5.large",
			ec2Client: &fakeEC2Client{
				output: &ec2.DescribeInstanceTypesOutput{
					InstanceTypes: []types.InstanceTypeInfo{},
				},
			},
			wantErr: true,
			errMsg:  "no instance type info returned",
		},
		{
			name:   "nil VCpuInfo returns error",
			vmType: "m5.large",
			ec2Client: &fakeEC2Client{
				output: &ec2.DescribeInstanceTypesOutput{
					InstanceTypes: []types.InstanceTypeInfo{
						{
							VCpuInfo: nil,
							MemoryInfo: &types.MemoryInfo{
								SizeInMiB: aws.Int64(8192),
							},
						},
					},
				},
			},
			wantErr: true,
			errMsg:  "missing vCPU info",
		},
		{
			name:   "nil MemoryInfo returns error",
			vmType: "m5.large",
			ec2Client: &fakeEC2Client{
				output: &ec2.DescribeInstanceTypesOutput{
					InstanceTypes: []types.InstanceTypeInfo{
						{
							VCpuInfo: &types.VCpuInfo{
								DefaultVCpus: aws.Int32(2),
							},
							MemoryInfo: nil,
						},
					},
				},
			},
			wantErr: true,
			errMsg:  "missing memory info",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			a := &AWS{ec2Client: tc.ec2Client}
			got, err := a.GetVMSpecs(context.Background(), tc.vmType)
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errMsg)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
