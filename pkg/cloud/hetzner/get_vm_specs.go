package hetzner

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
)

func (h *Hetzner) GetVMSpecs(ctx context.Context, machineType string) *cloud.VMSpec {
	machineDetails, _, err := h.hcloudClient.ServerType.GetByName(ctx, machineType)
	assert.AssertErrNil(ctx, err, "Failed getting machine details")
	assert.AssertNotNil(ctx, machineDetails, "Got empty machine details")

	return &cloud.VMSpec{
		CPU:            uint32(machineDetails.Cores),
		Memory:         uint32(machineDetails.Memory),
		RootVolumeSize: aws.Uint32(uint32(machineDetails.Disk)),
	}
}
