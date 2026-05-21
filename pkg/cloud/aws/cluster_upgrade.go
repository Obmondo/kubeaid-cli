// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package aws

import (
	"context"
	"fmt"

	yqCmdLib "github.com/mikefarah/yq/v4/cmd"
	"github.com/sagikazarmark/slog-shim"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	capaV1Beta2 "sigs.k8s.io/cluster-api-provider-aws/v2/api/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Obmondo/kubeaid-cli/pkg/utils/kubernetes"
)

type AWSMachineTemplateUpdates struct {
	AMIID string
}

func (*AWS) UpdateMachineTemplate(ctx context.Context,
	clusterClient client.Client,
	name string,
	updates any,
) error {
	parsedUpdates, ok := updates.(AWSMachineTemplateUpdates)
	if !ok {
		return fmt.Errorf("wrong type of MachineTemplateUpdates object passed")
	}

	awsMachineTemplate := &capaV1Beta2.AWSMachineTemplate{
		ObjectMeta: v1.ObjectMeta{
			Name:      name,
			Namespace: kubernetes.GetCapiClusterNamespace(),
		},
	}
	if err := kubernetes.GetKubernetesResource(ctx, clusterClient, awsMachineTemplate); err != nil {
		return fmt.Errorf("retrieving the current AWSMachineTemplate: %w", err)
	}

	if err := clusterClient.Delete(ctx, awsMachineTemplate, &client.DeleteOptions{}); err != nil {
		return fmt.Errorf("deleting the current AWSMachineTemplate: %w", err)
	}
	slog.InfoContext(ctx, "Deleted the current AWSMachineTemplate")

	awsMachineTemplate.Spec.Template.Spec.AMI.ID = &parsedUpdates.AMIID
	awsMachineTemplate.ResourceVersion = ""

	if err := clusterClient.Create(ctx, awsMachineTemplate, &client.CreateOptions{}); err != nil {
		return fmt.Errorf("recreating the AWSMachineTemplate: %w", err)
	}

	return nil
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
