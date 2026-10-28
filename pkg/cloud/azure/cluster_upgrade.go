// Copyright 2025 Obmondo
// SPDX-License-Identifier: Apache-2.0

package azure

import (
	"context"
	"fmt"

	yqCmdLib "github.com/mikefarah/yq/v4/cmd"
)

type AzureMachineTemplateUpdates struct {
	NewImageOffer string
}

func (a *Azure) UpdateCapiClusterValuesFile(ctx context.Context, path string, updates any) error {
	parsedUpdates, ok := updates.(AzureMachineTemplateUpdates)
	if !ok {
		return fmt.Errorf("wrong type of MachineTemplateUpdates object passed")
	}

	// The user doesn't want to do an OS upgrade.
	// So, we don't need to do anything.
	if len(parsedUpdates.NewImageOffer) == 0 {
		return nil
	}

	// Update the Canonical Ubuntu image offer.
	yqCmd := yqCmdLib.New()
	yqCmd.SetArgs([]string{
		"--in-place", "--yaml-output", "--yaml-roundtrip",

		fmt.Sprintf("(.azure.canonicalUbuntuImage.offer) = \"%s\"", parsedUpdates.NewImageOffer),

		path,
	})
	err := yqCmd.ExecuteContext(ctx)
	if err != nil {
		return fmt.Errorf("updating Canonical Ubuntu image offer in values-capi-cluster.yaml file: %w", err)
	}

	return nil
}
