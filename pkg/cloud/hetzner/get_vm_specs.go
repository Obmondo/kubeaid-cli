// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package hetzner

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud"
)

func (h *Hetzner) GetVMSpecs(ctx context.Context, machineType string) (*cloud.VMSpec, error) {
	machineDetails, _, err := h.serverTypeClient.GetByName(ctx, machineType)
	if err != nil {
		return nil, fmt.Errorf("getting machine details for %q: %w", machineType, err)
	}
	if machineDetails == nil {
		return nil, fmt.Errorf("machine type %q not found", machineType)
	}

	return &cloud.VMSpec{
		CPU:            uint32(machineDetails.Cores),
		Memory:         uint32(machineDetails.Memory),
		RootVolumeSize: aws.Uint32(uint32(machineDetails.Disk)),
	}, nil
}
