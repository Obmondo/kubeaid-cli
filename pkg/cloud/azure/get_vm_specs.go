// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package azure

import (
	"context"
	"log/slog"
	"os"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
)

func (a *Azure) GetVMSpecs(ctx context.Context, vmType string) *cloud.VMSpec {
	vmSizesListPager := a.vmSizesClient.NewListPager(
		config.ParsedGeneralConfig.Cloud.Azure.Location,
		nil,
	)

	for vmSizesListPager.More() {
		response, err := vmSizesListPager.NextPage(ctx)
		assert.AssertErrNil(ctx, err, "Failed fetching VM sizes list")

		for _, vmSize := range response.Value {
			if *vmSize.Name == vmType {
				return &cloud.VMSpec{
					CPU:    uint32(*vmSize.NumberOfCores),
					Memory: uint32(*vmSize.MemoryInMB) / 1024,
				}
			}
		}
	}

	slog.ErrorContext(ctx, "Failed getting VM specs", slog.String("vm-type", vmType))
	os.Exit(1)
	return nil
}
