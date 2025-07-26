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
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/kubernetes"
)

type AzureMachineTemplateUpdates struct {
	NewImageOffer string
}

func (*Azure) UpdateMachineTemplate(ctx context.Context,
	clusterClient client.Client,
	updates any,
) {
	parsedUpdates, ok := updates.(AzureMachineTemplateUpdates)
	assert.Assert(ctx, ok, "Wrong type of MachineTemplateUpdates object passed")

	// If the user doesn't want to do an OS upgrade,
	// then we don't need to do anything.
	if len(parsedUpdates.NewImageOffer) == 0 {
		return
	}

	// Get the AzureMachineTemplate currently being referred by KubeadmControlPlane.
	azureMachineTemplate := &capzV1Beta1.AzureMachineTemplate{
		ObjectMeta: v1.ObjectMeta{
			Name:      fmt.Sprintf("%s-control-plane", config.ParsedGeneralConfig.Cluster.Name),
			Namespace: kubernetes.GetCapiClusterNamespace(),
		},
	}
	err := kubernetes.GetKubernetesResource(ctx, clusterClient, azureMachineTemplate)
	assert.AssertErrNil(ctx, err,
		"Failed retrieving the current AzureMachineTemplate resource used by the KubeadmControlPlane resource",
	)

	// Delete that AzureMachineTemplate.
	err = clusterClient.Delete(ctx, azureMachineTemplate, &client.DeleteOptions{})
	assert.AssertErrNil(ctx, err,
		"Failed deleting the current AzureMachineTemplate resource being used by the KubeadmControlPlane resource",
	)
	slog.InfoContext(ctx,
		"Deleted the current azureMachineTemplate resource being used by the KubeadmControlPlane resource",
	)

	// Recreate the updated AzureMachineTemplate.

	azureMachineTemplate.Spec.Template.Spec.Image.Marketplace.Offer = parsedUpdates.NewImageOffer
	azureMachineTemplate.ResourceVersion = ""

	err = clusterClient.Create(ctx, azureMachineTemplate, &client.CreateOptions{})
	assert.AssertErrNil(ctx, err, "Failed recreating the AzureMachineTemplate")
}

func (a *Azure) UpdateCapiClusterValuesFileWithCloudSpecificDetails(ctx context.Context,
	capiClusterValuesFilePath string,
	updates any,
) {
	parsedUpdates, ok := updates.(AzureMachineTemplateUpdates)
	assert.Assert(ctx, ok, "Wrong type of MachineTemplateUpdates object passed")

	// If the user doesn't want to do an OS upgrade,
	// then we don't need to do anything.
	if len(parsedUpdates.NewImageOffer) == 0 {
		return
	}

	// Update the Canonical Ubuntu image offer.
	yqCmd := yqCmdLib.New()
	yqCmd.SetArgs([]string{
		"--in-place", "--yaml-output", "--yaml-roundtrip",

		fmt.Sprintf("(.azure.canonicalUbuntuImage.offer) = \"%s\"", parsedUpdates.NewImageOffer),

		capiClusterValuesFilePath,
	})
	err := yqCmd.ExecuteContext(ctx)
	assert.AssertErrNil(ctx, err,
		"Failed updating Canonical Ubuntu image offer, in values-capi-cluster.yaml file",
	)
}
