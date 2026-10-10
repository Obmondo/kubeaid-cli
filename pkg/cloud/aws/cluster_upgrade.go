// Copyright 2025 Obmondo
// SPDX-License-Identifier: Apache-2.0

package aws

import (
	"context"
	"fmt"

	yqCmdLib "github.com/mikefarah/yq/v4/cmd"
)

type AWSMachineTemplateUpdates struct {
	AMIID string
}

func (*AWS) UpdateCapiClusterValuesFile(ctx context.Context, path string, updates any) error {
	parsedUpdates, ok := updates.(AWSMachineTemplateUpdates)
	if !ok {
		return fmt.Errorf("wrong type of MachineTemplateUpdates object passed")
	}

	yqCmd := yqCmdLib.New()
	yqCmd.SetArgs([]string{
		"--in-place", "--yaml-output", "--yaml-roundtrip",
		fmt.Sprintf("(.aws.controlPlane.ami.id) = \"%s\"", parsedUpdates.AMIID),
		path,
	})
	if err := yqCmd.ExecuteContext(ctx); err != nil {
		return fmt.Errorf("updating AMI ID for control-plane nodes in values-capi-cluster.yaml: %w", err)
	}

	yqCmd = yqCmdLib.New()
	yqCmd.SetArgs([]string{
		"--in-place", "--yaml-output", "--yaml-roundtrip",
		fmt.Sprintf("(.aws.nodeGroups[].ami.id) = \"%s\"", parsedUpdates.AMIID),
		path,
	})
	if err := yqCmd.ExecuteContext(ctx); err != nil {
		return fmt.Errorf("updating AMI ID for nodegroups in values-capi-cluster.yaml: %w", err)
	}

	return nil
}
