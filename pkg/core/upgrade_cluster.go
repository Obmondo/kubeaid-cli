package core

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/Obmondo/kubeaid-bootstrap-script/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud"
	"github.com/Obmondo/kubeaid-bootstrap-script/utils"
	"github.com/Obmondo/kubeaid-bootstrap-script/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/utils/logger"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterAPIV1Beta1 "sigs.k8s.io/cluster-api/api/v1beta1"
	kcpV1Beta1 "sigs.k8s.io/cluster-api/controlplane/kubeadm/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type UpgradeClusterArgs struct {
	NewKubernetesVersion string

	CloudProvider        cloud.CloudProvider
	CloudSpecificUpdates any
}

func UpgradeCluster(ctx context.Context, args UpgradeClusterArgs) {
	var clusterClient client.Client
	{
		provisionedClusterClient, _ := utils.CreateKubernetesClient(ctx, constants.OutputPathProvisionedClusterKubeconfig, true)
		clusterClient = provisionedClusterClient

		// If `clusterctl move` wasn't executed, then we need to communicate with the management
		// cluster instead.
		if !utils.IsClusterctlMoveExecuted(ctx, provisionedClusterClient) {
			managementClusterClient, _ := utils.CreateKubernetesClient(ctx, constants.OutputPathManagementClusterKubeconfig, true)
			clusterClient = managementClusterClient
		}
	}

	// (1) Upgrading the Control Plane.
	{
		slog.InfoContext(ctx, "🔧 Upgrading control plane....")

		// Delete and recreate the corresponding machine template with updated options (like AMI in
		// case of AWS).
		// NOTE : Since machine templates are immutable, we cannot directly update them.
		//
		// REFER : https://cluster-api.sigs.k8s.io/tasks/upgrading-clusters#upgrading-the-control-plane-machines.
		args.CloudProvider.UpdateMachineTemplate(ctx, clusterClient, args.CloudSpecificUpdates)
		slog.InfoContext(ctx,
			"Recreated the AWSMachineTemplate resource used by the KubeadmControlPlane resource",
		)

		// Update the Kubernetes version in the KubeadmControlPlane resource.

		kubeadmControlPlaneName := fmt.Sprintf("%s-control-plane", config.ParsedConfig.Cluster.Name)

		kubeadmControlPlane := &kcpV1Beta1.KubeadmControlPlane{
			ObjectMeta: v1.ObjectMeta{
				Name:      kubeadmControlPlaneName,
				Namespace: utils.GetCapiClusterNamespace(),
			},
		}
		err := utils.GetKubernetesResource(ctx, clusterClient, kubeadmControlPlane)
		assert.AssertErrNil(ctx, err, "Failed retrieving KubeadmControlPlane")

		kubeadmControlPlane.Spec.Version = args.NewKubernetesVersion

		err = clusterClient.Update(ctx, kubeadmControlPlane, nil)
		assert.AssertErrNil(ctx, err, "Failed updating Kubernetes version in KubeadmControlPlane")

		// Ensure that changes to the control-plane start to roll out immediately.
		utils.ExecuteCommandOrDie(fmt.Sprintf(
			"clusterctl alpha rollout restart kubeadmcontrolplane/%s -n %s",
			kubeadmControlPlane.GetName(), kubeadmControlPlane.GetNamespace(),
		))
	}

	// (2) Upgrading each node-group one by one.
	for _, nodeGroup := range config.ParsedConfig.Cloud.AWS.NodeGroups {
		ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
			slog.String("node-group", nodeGroup.Name),
		})

		slog.InfoContext(ctx, "🔧 Upgrading node-group....")

		// Delete and recreate the corresponding machine template with updated options.
		args.CloudProvider.UpdateMachineTemplate(ctx, clusterClient, args.CloudSpecificUpdates)

		// Update the corresponding MachineDeployment.

		machineDeploymentName := fmt.Sprintf("%s-%s", config.ParsedConfig.Cluster.Name, nodeGroup.Name)

		machineDeployment := &clusterAPIV1Beta1.MachineDeployment{
			ObjectMeta: v1.ObjectMeta{
				Name:      machineDeploymentName,
				Namespace: utils.GetCapiClusterNamespace(),
			},
		}
		err := utils.GetKubernetesResource(ctx, clusterClient, machineDeployment)
		assert.AssertErrNil(ctx, err, "Failed retrieving MachineDeployment resource")

		machineDeployment.Spec.Template.Spec.Version = &args.NewKubernetesVersion

		err = clusterClient.Update(ctx, machineDeployment, nil)
		assert.AssertErrNil(ctx, err, "Failed updating Kubernetes version in MachineDeployment")

		// Ensure that changes to the node-group start to roll out immediately.
		utils.ExecuteCommandOrDie(fmt.Sprintf(
			"clusterctl alpha rollout restart machinedeployment/%s -n %s",
			machineDeployment.GetName(), machineDeployment.GetNamespace(),
		))
	}
}
