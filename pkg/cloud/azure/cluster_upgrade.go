package azure

import (
	"context"
	"fmt"

	"github.com/sagikazarmark/slog-shim"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	capzV1Beta1 "sigs.k8s.io/cluster-api-provider-azure/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/kubernetes"
)

type AzureMachineTemplateUpdates struct {
	ImageID string
}

func (*Azure) UpdateMachineTemplate(ctx context.Context,
	clusterClient client.Client,
	updates any,
) {
	parsedUpdates, ok := updates.(AzureMachineTemplateUpdates)
	assert.Assert(ctx, ok, "Wrong type of MachineTemplateUpdates object passed")

	// Get the AzureMachineTemplate currently being referred by KubeadmControlPlane.
	azureMachineTemplate := &capzV1Beta1.AzureMachineTemplate{
		ObjectMeta: v1.ObjectMeta{
			Name:      fmt.Sprintf("%s-control-plane", config.ParsedGeneralConfig.Cluster.Name),
			Namespace: kubernetes.GetCapiClusterNamespace(),
		},
	}
	err := kubernetes.GetKubernetesResource(ctx, clusterClient, azureMachineTemplate)
	assert.AssertErrNil(
		ctx,
		err,
		"Failed retrieving the current AzureMachineTemplate resource used by the KubeadmControlPlane resource",
	)

	// Delete that AzureMachineTemplate.
	err = clusterClient.Delete(ctx, azureMachineTemplate, &client.DeleteOptions{})
	assert.AssertErrNil(
		ctx,
		err,
		"Failed deleting the current azureMachineTemplate resource used by the KubeadmControlPlane resource",
	)
	slog.InfoContext(
		ctx,
		"Deleted the current azureMachineTemplate resource used by the KubeadmControlPlane resource",
	)

	// Recreate the updated AzureMachineTemplate.

	azureMachineTemplate.Spec.Template.Spec.Image.ID = &parsedUpdates.ImageID
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

	// Update the image ID.
	_ = utils.ExecuteCommandOrDie(fmt.Sprintf(
		"yq -i -y '(.azure.imageID) = \"%s\"' %s",
		parsedUpdates.ImageID, capiClusterValuesFilePath,
	))
}
