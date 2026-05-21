// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package hetzner

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/client"

	caphV1Beta1 "github.com/syself/cluster-api-provider-hetzner/api/v1beta1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeadmControlPlaneV1Beta1 "sigs.k8s.io/cluster-api/api/controlplane/kubeadm/v1beta1"
	clusterAPIV1Beta1 "sigs.k8s.io/cluster-api/api/core/v1beta1"

	yqCmdLib "github.com/mikefarah/yq/v4/cmd"

	"github.com/Obmondo/kubeaid-cli/pkg/utils/kubernetes"
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

func (*Hetzner) UpdateMachineTemplate(ctx context.Context,
	clusterClient client.Client,
	name string,
	updates any,
) error {
	parsedUpdates, ok := updates.(HetznerMachineTemplateUpdates)
	if !ok {
		return fmt.Errorf("wrong type of MachineTemplateUpdates object passed")
	}

	var infrastructureRefKind string

	switch strings.Contains(name, "control-plane") {
	case true:
		kubeadmControlPlane := &kubeadmControlPlaneV1Beta1.KubeadmControlPlane{
			ObjectMeta: metaV1.ObjectMeta{
				Name:      name,
				Namespace: kubernetes.GetCapiClusterNamespace(),
			},
		}
		if err := kubernetes.GetKubernetesResource(ctx, clusterClient, kubeadmControlPlane); err != nil {
			return fmt.Errorf("retrieving the corresponding KubeadmControlPlane: %w", err)
		}

		infrastructureRefKind = kubeadmControlPlane.Spec.MachineTemplate.InfrastructureRef.Kind

	default:
		machineDeployment := &clusterAPIV1Beta1.MachineDeployment{
			ObjectMeta: metaV1.ObjectMeta{
				Name:      name,
				Namespace: kubernetes.GetCapiClusterNamespace(),
			},
		}
		if err := kubernetes.GetKubernetesResource(ctx, clusterClient, machineDeployment); err != nil {
			return fmt.Errorf("retrieving the corresponding MachineDeployment: %w", err)
		}

		infrastructureRefKind = machineDeployment.Spec.Template.Spec.InfrastructureRef.Kind
	}

	switch infrastructureRefKind {
	case "HCloudMachineTemplate":
		return updateHCloudMachineTemplate(ctx, clusterClient, name, parsedUpdates.HCloudMachineTemplateUpdates)

	case "HetznerBareMetalMachineTemplate":
		return updateHetznerBareMetalMachineTemplate(ctx, clusterClient, name,
			parsedUpdates.HetznerBareMetalMachineTemplateUpdates,
		)

	default:
		return fmt.Errorf("unexpected infrastructureRef kind %q in MachineDeployment", infrastructureRefKind)
	}
}

func updateHCloudMachineTemplate(ctx context.Context,
	clusterClient client.Client,
	name string,
	updates HCloudMachineTemplateUpdates,
) error {
	hcloudMachineTemplate := &caphV1Beta1.HCloudMachineTemplate{
		ObjectMeta: metaV1.ObjectMeta{
			Name:      name,
			Namespace: kubernetes.GetCapiClusterNamespace(),
		},
	}
	if err := kubernetes.GetKubernetesResource(ctx, clusterClient, hcloudMachineTemplate); err != nil {
		return fmt.Errorf("retrieving the current HCloudMachineTemplate: %w", err)
	}

	if err := clusterClient.Delete(ctx, hcloudMachineTemplate, &client.DeleteOptions{}); err != nil {
		return fmt.Errorf("deleting the current HCloudMachineTemplate: %w", err)
	}
	slog.InfoContext(ctx, "Deleted the current HCloudMachineTemplate")

	hcloudMachineTemplate.Spec.Template.Spec.ImageName = updates.NewImageName
	hcloudMachineTemplate.ResourceVersion = ""

	if err := clusterClient.Create(ctx, hcloudMachineTemplate, &client.CreateOptions{}); err != nil {
		return fmt.Errorf("recreating the HCloudMachineTemplate: %w", err)
	}

	return nil
}

func updateHetznerBareMetalMachineTemplate(ctx context.Context,
	clusterClient client.Client,
	name string,
	updates HetznerBareMetalMachineTemplateUpdates,
) error {
	hetznerBareMetalMachineTemplate := &caphV1Beta1.HetznerBareMetalMachineTemplate{
		ObjectMeta: metaV1.ObjectMeta{
			Name:      name,
			Namespace: kubernetes.GetCapiClusterNamespace(),
		},
	}
	if err := kubernetes.GetKubernetesResource(ctx, clusterClient, hetznerBareMetalMachineTemplate); err != nil {
		return fmt.Errorf("retrieving the current HetznerBareMetalMachineTemplate: %w", err)
	}

	if err := clusterClient.Delete(ctx, hetznerBareMetalMachineTemplate, &client.DeleteOptions{}); err != nil {
		return fmt.Errorf("deleting the current HetznerBareMetalMachineTemplate: %w", err)
	}
	slog.InfoContext(ctx, "Deleted the current HetznerBareMetalMachineTemplate")

	hetznerBareMetalMachineTemplate.Spec.Template.Spec.InstallImage.Image.Path = updates.NewImagePath
	hetznerBareMetalMachineTemplate.ResourceVersion = ""

	if err := clusterClient.Create(ctx, hetznerBareMetalMachineTemplate, &client.CreateOptions{}); err != nil {
		return fmt.Errorf("recreating the HetznerBareMetalMachineTemplate: %w", err)
	}

	return nil
}

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
