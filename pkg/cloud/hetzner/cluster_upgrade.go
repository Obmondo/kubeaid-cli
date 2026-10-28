// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package hetzner

import (
	"context"
	"fmt"

	yqCmdLib "github.com/mikefarah/yq/v4/cmd"
)

type (
	HetznerMachineTemplateUpdates struct {
		HCloudMachineTemplateUpdates
		HetznerBareMetalMachineTemplateUpdates
	}

	HCloudMachineTemplateUpdates struct {
		NewImageName string
	}

	HetznerBareMetalMachineTemplateUpdates struct {
		NewImagePath string
	}
)

func (*Hetzner) UpdateCapiClusterValuesFile(ctx context.Context, path string, updates any) error {
	parsedUpdates, ok := updates.(HetznerMachineTemplateUpdates)
	if !ok {
		return fmt.Errorf("wrong type of MachineTemplateUpdates object passed")
	}

	if len(parsedUpdates.NewImageName) > 0 {
		yqCmd := yqCmdLib.New()
		yqCmd.SetArgs([]string{
			"eval",
			fmt.Sprintf("(.hetzner.hcloud.imageName) = \"%s\"", parsedUpdates.NewImageName),
			path,
			"--inplace",
		})
		if err := yqCmd.ExecuteContext(ctx); err != nil {
			return fmt.Errorf("updating image name for HCloud machines in values-capi-cluster.yaml: %w", err)
		}
	}

	if len(parsedUpdates.NewImagePath) > 0 {
		yqCmd := yqCmdLib.New()
		yqCmd.SetArgs([]string{
			"eval",
			fmt.Sprintf("(.hetzner.bareMetal.installImage.imagePath) = \"%s\"", parsedUpdates.NewImagePath),
			path,
			"--inplace",
		})
		if err := yqCmd.ExecuteContext(ctx); err != nil {
			return fmt.Errorf("updating install-image script path in values-capi-cluster.yaml: %w", err)
		}
	}

	return nil
}
