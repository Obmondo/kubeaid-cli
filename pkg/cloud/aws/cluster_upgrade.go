package aws

import (
	"context"
	"fmt"

	"github.com/sagikazarmark/slog-shim"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	capaV1Beta2 "sigs.k8s.io/cluster-api-provider-aws/v2/api/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/kubernetes"
)

type AWSMachineTemplateUpdates struct {
	AMIID string
}

func (*AWS) UpdateMachineTemplate(ctx context.Context, clusterClient client.Client, updates any) {
	parsedUpdates, ok := updates.(AWSMachineTemplateUpdates)
	assert.Assert(ctx, ok, "Wrong type of MachineTemplateUpdates object passed")

	// Get the AWSMachineTemplate currently being referred by the KubeadmControlPlane.
	awsMachineTemplate := &capaV1Beta2.AWSMachineTemplate{
		ObjectMeta: v1.ObjectMeta{
			Name:      fmt.Sprintf("%s-control-plane", config.ParsedGeneralConfig.Cluster.Name),
			Namespace: kubernetes.GetCapiClusterNamespace(),
		},
	}
	err := kubernetes.GetKubernetesResource(ctx, clusterClient, awsMachineTemplate)
	assert.AssertErrNil(
		ctx,
		err,
		"Failed retrieving the current AWSMachineTemplate resource used by the KubeadmControlPlane resource",
	)

	// Delete that AWSMachineTemplate.
	err = clusterClient.Delete(ctx, awsMachineTemplate, &client.DeleteOptions{})
	assert.AssertErrNil(
		ctx,
		err,
		"Failed deleting the current AWSMachineTemplate resource used by the KubeadmControlPlane resource",
	)
	slog.InfoContext(ctx,
		"Deleted the current AWSMachineTemplate resource used by the KubeadmControlPlane resource",
	)

	// Recreate the updated AWSMachineTemplate.

	awsMachineTemplate.Spec.Template.Spec.AMI.ID = &parsedUpdates.AMIID
	awsMachineTemplate.ResourceVersion = ""

	err = clusterClient.Create(ctx, awsMachineTemplate, &client.CreateOptions{})
	assert.AssertErrNil(ctx, err, "Failed recreating the AWSMachineTemplate")
}

func (*AWS) UpdateCapiClusterValuesFileWithCloudSpecificDetails(ctx context.Context,
	capiClusterValuesFilePath string,
	updates any,
) {
	parsedUpdates, ok := updates.(AWSMachineTemplateUpdates)
	assert.Assert(ctx, ok, "Wrong type of MachineTemplateUpdates object passed")

	// Update AMI ID for the Control Plane.
	_ = utils.ExecuteCommandOrDie(fmt.Sprintf(
		"yq -i -y '(.aws.controlPlane.ami.id) = \"%s\"' %s",
		parsedUpdates.AMIID, capiClusterValuesFilePath,
	))

	// Update AMI ID in each node-group definition.
	_ = utils.ExecuteCommandOrDie(fmt.Sprintf(
		"yq -i -y '(.aws.nodeGroups[].ami.id) = \"%s\"' %s",
		parsedUpdates.AMIID, capiClusterValuesFilePath,
	))
}
