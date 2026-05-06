// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package azure

import (
	"context"
	"fmt"

	yqCmdLib "github.com/mikefarah/yq/v4/cmd"
	"github.com/sagikazarmark/slog-shim"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	capzV1Beta1 "sigs.k8s.io/cluster-api-provider-azure/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/kubernetes"
)

type AzureMachineTemplateUpdates struct {
	NewImageOffer string
}

func (*Azure) UpdateMachineTemplate(ctx context.Context,
	clusterClient client.Client,
	name string,
	updates any,
) error {
	parsedUpdates, ok := updates.(AzureMachineTemplateUpdates)
	if !ok {
		return fmt.Errorf("wrong type of MachineTemplateUpdates object passed")
	}

	// The user doesn't want to do an OS upgrade.
	// So we don't need to do anything.
	if len(parsedUpdates.NewImageOffer) == 0 {
		return nil
	}

	// Get the AzureMachineTemplate currently being referred by KubeadmControlPlane.
	azureMachineTemplate := &capzV1Beta1.AzureMachineTemplate{
		ObjectMeta: v1.ObjectMeta{
			Name:      fmt.Sprintf("%s-control-plane", config.ParsedGeneralConfig.Cluster.Name),
			Namespace: kubernetes.GetCapiClusterNamespace(),
		},
	}
	err := kubernetes.GetKubernetesResource(ctx, clusterClient, azureMachineTemplate)
	if err != nil {
		return fmt.Errorf("retrieving the current AzureMachineTemplate resource used by the KubeadmControlPlane resource: %w", err)
	}

	// Delete that AzureMachineTemplate.
	err = clusterClient.Delete(ctx, azureMachineTemplate, &client.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("deleting the current AzureMachineTemplate resource being used by the KubeadmControlPlane resource: %w", err)
	}
	slog.InfoContext(ctx,
		"Deleted the current azureMachineTemplate resource being used by the KubeadmControlPlane resource",
	)

	// Recreate the updated AzureMachineTemplate.

	azureMachineTemplate.Spec.Template.Spec.Image.Marketplace.Offer = parsedUpdates.NewImageOffer
	azureMachineTemplate.ResourceVersion = ""

	err = clusterClient.Create(ctx, azureMachineTemplate, &client.CreateOptions{})
	if err != nil {
		return fmt.Errorf("recreating the AzureMachineTemplate: %w", err)
	}

	return nil
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
