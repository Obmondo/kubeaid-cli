package aws

import (
	"context"
	"log/slog"

	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
)

func (a *AWS) GetVMSpecs(ctx context.Context, vmType string) *cloud.VMSpec {
	instanceType := types.InstanceType(vmType)

	output, err := a.ec2Client.DescribeInstanceTypes(ctx, &ec2.DescribeInstanceTypesInput{
		InstanceTypes: []types.InstanceType{
			instanceType,
		},
	})
	assert.AssertErrNil(ctx, err,
		"Failed to describe EC2 instance type",
		slog.String("instance-type", vmType),
	)

	instanceDetails := output.InstanceTypes[0]

	return &cloud.VMSpec{
		CPU:    uint32(*instanceDetails.VCpuInfo.DefaultVCpus),
		Memory: uint32(*instanceDetails.MemoryInfo.SizeInMiB),
	}
}
