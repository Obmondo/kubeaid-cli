// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud"
)

type ec2DescribeInstanceTypesAPI interface {
	DescribeInstanceTypes(ctx context.Context, params *ec2.DescribeInstanceTypesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeInstanceTypesOutput, error)
}

func (a *AWS) GetVMSpecs(ctx context.Context, vmType string) (*cloud.VMSpec, error) {
	instanceType := types.InstanceType(vmType)

	output, err := a.ec2Client.DescribeInstanceTypes(ctx, &ec2.DescribeInstanceTypesInput{
		InstanceTypes: []types.InstanceType{
			instanceType,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("describing EC2 instance type %s: %w", vmType, err)
	}

	if len(output.InstanceTypes) == 0 {
		return nil, fmt.Errorf("no instance type info returned for %s", vmType)
	}

	instanceDetails := output.InstanceTypes[0]

	if instanceDetails.VCpuInfo == nil || instanceDetails.VCpuInfo.DefaultVCpus == nil {
		return nil, fmt.Errorf("missing vCPU info for instance type %s", vmType)
	}
	if instanceDetails.MemoryInfo == nil || instanceDetails.MemoryInfo.SizeInMiB == nil {
		return nil, fmt.Errorf("missing memory info for instance type %s", vmType)
	}

	return &cloud.VMSpec{
		CPU:    uint32(*instanceDetails.VCpuInfo.DefaultVCpus),
		Memory: uint32(*instanceDetails.MemoryInfo.SizeInMiB) / 1024,
	}, nil
}
