// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package hetzner

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/client"

	caphV1Beta1 "github.com/syself/cluster-api-provider-hetzner/api/v1beta1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterAPIV1Beta1 "sigs.k8s.io/cluster-api/api/v1beta1"
	kubeadmControlPlaneV1Beta1 "sigs.k8s.io/cluster-api/controlplane/kubeadm/api/v1beta1"

	yqCmdLib "github.com/mikefarah/yq/v4/cmd"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/kubernetes"
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
) {
	parsedUpdates, ok := updates.(HetznerMachineTemplateUpdates)
	assert.Assert(ctx, ok, "Wrong type of MachineTemplateUpdates object passed")

	// Determine whether the machine template corresponds to HCloud or Hetzner Bare Metal.

	var infrastructureRefKind string

	switch strings.Contains(name, "control-plane") {
	// Machine template for the control-plane.
	case true:
		// Get the KubeadmControlPlane resource, from which we can determine :
		// whether the control-plane nodes are in HCloud or Hetzner Bare Metal.

		kubeadmControlPlane := &kubeadmControlPlaneV1Beta1.KubeadmControlPlane{
			ObjectMeta: metaV1.ObjectMeta{
				Name:      name,
				Namespace: kubernetes.GetCapiClusterNamespace(),
			},
		}
		err := kubernetes.GetKubernetesResource(ctx, clusterClient, kubeadmControlPlane)
		assert.AssertErrNil(ctx, err, "Failed retrieving the corresponding KubeadmControlPlane")

		infrastructureRefKind = kubeadmControlPlane.Spec.MachineTemplate.InfrastructureRef.Kind

	// Machine template for a node-group.
	default:
		// Get the MachineDeployment resource, from which we can determine :
		// whether the node-group is in HCloud or Hetzner Bare Metal.

		machineDeployment := &clusterAPIV1Beta1.MachineDeployment{
			ObjectMeta: metaV1.ObjectMeta{
				Name:      name,
				Namespace: kubernetes.GetCapiClusterNamespace(),
			},
		}
		err := kubernetes.GetKubernetesResource(ctx, clusterClient, machineDeployment)
		assert.AssertErrNil(ctx, err, "Failed retrieving the corresponding MachineDeployment")

		infrastructureRefKind = machineDeployment.Spec.Template.Spec.InfrastructureRef.Kind
	}

	switch infrastructureRefKind {
	case "HCloudMachineTemplate":
		updateHCloudMachineTemplate(ctx, clusterClient, name, parsedUpdates.HCloudMachineTemplateUpdates)

	case "HetznerBareMetalMachineTemplate":
		updateHetznerBareMetalMachineTemplate(ctx, clusterClient, name,
			parsedUpdates.HetznerBareMetalMachineTemplateUpdates,
		)

	default:
		slog.InfoContext(ctx, "Wrong type of infrastructureRef kind in MachineDeployment",
			slog.String("kind", infrastructureRefKind),
		)
		os.Exit(1)
	}
}

func updateHCloudMachineTemplate(ctx context.Context,
	clusterClient client.Client,
	name string,
	updates HCloudMachineTemplateUpdates,
) {
	// Get the HCloudMachineTemplate.
	hcloudMachineTemplate := &caphV1Beta1.HCloudMachineTemplate{
		ObjectMeta: metaV1.ObjectMeta{
			Name:      name,
			Namespace: kubernetes.GetCapiClusterNamespace(),
		},
	}
	err := kubernetes.GetKubernetesResource(ctx, clusterClient, hcloudMachineTemplate)
	assert.AssertErrNil(ctx, err, "Failed retrieving the current HCloudMachineTemplate")

	// Delete that.
	err = clusterClient.Delete(ctx, hcloudMachineTemplate, &client.DeleteOptions{})
	assert.AssertErrNil(ctx, err, "Failed deleting the current HCloudMachineTemplate")
	slog.InfoContext(ctx, "Deleted the current HCloudMachineTemplate")

	// And, recreate it, with the updates.

	hcloudMachineTemplate.Spec.Template.Spec.ImageName = updates.NewImageName
	hcloudMachineTemplate.ResourceVersion = ""

	err = clusterClient.Create(ctx, hcloudMachineTemplate, &client.CreateOptions{})
	assert.AssertErrNil(ctx, err, "Failed recreating the HCloudMachineTemplate")
}

func updateHetznerBareMetalMachineTemplate(ctx context.Context,
	clusterClient client.Client,
	name string,
	updates HetznerBareMetalMachineTemplateUpdates,
) {
	// Get the HetznerBareMetalMachineTemplate.
	hetznerBareMetalMachineTemplate := &caphV1Beta1.HetznerBareMetalMachineTemplate{
		ObjectMeta: metaV1.ObjectMeta{
			Name:      name,
			Namespace: kubernetes.GetCapiClusterNamespace(),
		},
	}
	err := kubernetes.GetKubernetesResource(ctx, clusterClient, hetznerBareMetalMachineTemplate)
	assert.AssertErrNil(ctx, err, "Failed retrieving the current HetznerBareMetalMachineTemplate")

	// Delete that.
	err = clusterClient.Delete(ctx, hetznerBareMetalMachineTemplate, &client.DeleteOptions{})
	assert.AssertErrNil(ctx, err, "Failed deleting the current HetznerBareMetalMachineTemplate")
	slog.InfoContext(ctx, "Deleted the current HetznerBareMetalMachineTemplate")

	// And, recreate it, with the updates.

	hetznerBareMetalMachineTemplate.Spec.Template.Spec.InstallImage.Image.Path = updates.NewImagePath
	hetznerBareMetalMachineTemplate.ResourceVersion = ""

	err = clusterClient.Create(ctx, hetznerBareMetalMachineTemplate, &client.CreateOptions{})
	assert.AssertErrNil(ctx, err, "Failed recreating the HetznerBareMetalMachineTemplate")
}

func (*Hetzner) UpdateCapiClusterValuesFile(ctx context.Context, path string, updates any) {
	parsedUpdates, ok := updates.(HetznerMachineTemplateUpdates)
	assert.Assert(ctx, ok, "Wrong type of MachineTemplateUpdates object passed")

	if len(parsedUpdates.NewImageName) > 0 {
		yqCmd := yqCmdLib.New()
		yqCmd.SetArgs([]string{
			"eval",
			fmt.Sprintf("(.hetzner.hcloud.imageName) = \"%s\"", parsedUpdates.NewImageName),
			path,
			"--inplace",
		})
		err := yqCmd.ExecuteContext(ctx)
		assert.AssertErrNil(ctx, err,
			"Failed updating image name for HCloud machines, in values-capi-cluster.yaml file",
		)
	}

	if len(parsedUpdates.NewImagePath) > 0 {
		yqCmd := yqCmdLib.New()
		yqCmd.SetArgs([]string{
			"eval",
			fmt.Sprintf("(.hetzner.bareMetal.installImage.imagePath) = \"%s\"", parsedUpdates.NewImagePath),
			path,
			"--inplace",
		})
		err := yqCmd.ExecuteContext(ctx)
		assert.AssertErrNil(ctx, err,
			"Failed updating install-image script path, in values-capi-cluster.yaml file",
		)
	}
}
