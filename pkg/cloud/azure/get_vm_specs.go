// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package azure

import (
	"context"
	"fmt"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
)

func (a *Azure) GetVMSpecs(ctx context.Context, vmType string) (*cloud.VMSpec, error) {
	vmSizes, err := a.listVMSizes(ctx, config.ParsedGeneralConfig.Cloud.Azure.Location)
	if err != nil {
		return nil, fmt.Errorf("fetching VM sizes list: %w", err)
	}

	for _, vmSize := range vmSizes {
		if *vmSize.Name == vmType {
			return &cloud.VMSpec{
				CPU:    uint32(*vmSize.NumberOfCores),
				Memory: uint32(*vmSize.MemoryInMB) / 1024,
			}, nil
		}
	}

	return nil, fmt.Errorf("VM type %q not found", vmType)
}
