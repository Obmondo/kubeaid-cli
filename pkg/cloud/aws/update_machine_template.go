package aws

import (
	"context"
	"fmt"

	"github.com/Obmondo/kubeaid-bootstrap-script/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/utils"
	"github.com/Obmondo/kubeaid-bootstrap-script/utils/assert"
	"github.com/sagikazarmark/slog-shim"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	capaV1Beta1 "sigs.k8s.io/cluster-api-provider-aws/v2/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type MachineTemplateUpdates struct {
	AMIID string
}

func (*AWS) UpdateMachineTemplate(ctx context.Context, clusterClient client.Client, _updates any) {
	updates, ok := _updates.(MachineTemplateUpdates)
	assert.Assert(ctx, ok, "Wrong type of MachineTemplateUpdates object passed")

	// Get the AWSMachineTemplate resource referred by KubeadmControlPlane resource.
	awsMachineTemplate := &capaV1Beta1.AWSMachineTemplate{
		ObjectMeta: v1.ObjectMeta{
			Name:      fmt.Sprintf("%s-control-plane", config.ParsedConfig.Cluster.Name),
			Namespace: utils.GetCapiClusterNamespace(),
		},
	}
	err := utils.GetKubernetesResource(ctx, clusterClient, awsMachineTemplate)
	assert.AssertErrNil(ctx, err,
		"Failed retrieving the current AWSMachineTemplate resource used by the KubeadmControlPlane resource",
	)

	// Delete that AWSMachineTemplate.
	err = clusterClient.Delete(ctx, awsMachineTemplate, nil)
	assert.AssertErrNil(ctx, err,
		"Failed deleting the current AWSMachineTemplate resource used by the KubeadmControlPlane resource",
	)
	slog.InfoContext(ctx,
		"Deleted the current AWSMachineTemplate resource used by the KubeadmControlPlane resource",
	)

	// Recreate the AWSMachineTemplate.
	awsMachineTemplate.Spec.Template.Spec.AMI = capaV1Beta1.AMIReference{
		ID: &updates.AMIID,
	}
	err = clusterClient.Create(ctx, awsMachineTemplate, nil)
	assert.AssertErrNil(ctx, err, "Failed recreating the AWSMachineTemplate")
}
