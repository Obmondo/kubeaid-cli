package core

import (
	"context"
	"log/slog"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	clusterAPIV1Beta1 "sigs.k8s.io/cluster-api/api/v1beta1"
	clusterctlClientLib "sigs.k8s.io/cluster-api/cmd/clusterctl/client"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/kubernetes"
)

func DeleteCluster(ctx context.Context) {
	cluster := &clusterAPIV1Beta1.Cluster{
		ObjectMeta: v1.ObjectMeta{
			Name:      config.ParsedGeneralConfig.Cluster.Name,
			Namespace: kubernetes.GetCapiClusterNamespace(),
		},
	}

	managementClusterKubeconfigPath := kubernetes.GetManagementClusterKubeconfigPath(ctx)

	//nolint:godox
	/*
	  BUG :

	  Suppose this command is running not on the original management cluster, but on a dev
	  environment that the user has created later. There can be 2 scenarios :

	    (1) clusterctl move was executed while provisioning the cluster. Then, we'll re-execute
	        clusterctl move, moving back the ClusterAPI manifests from the provisioned to the
	        management cluster.

	    (2) clusterctl move wasn't executed while provisioning the cluster. In that case, how are
	        we going to have those ClusterAPI resource manifests back in the cluster? Should we
	        sync the whole capi-cluster ArgoCD App? I need to test this.
	*/

	// Detect whether the 'clusterctl move' command has already been executed or not.
	if kubernetes.IsClusterctlMoveExecuted(ctx) {
		slog.InfoContext(ctx, "Detected that the 'clusterctl move' command has been executed")

		/*
		   Pause the ClusterAPI Infrastructure Provider in the main cluster,
		   and move back the ClusterAPI manifests to the management cluster. They will be processed by
		   the management cluster's Infrastructure Provider.

		   NOTE : We need to retry, since we can get 'failed to call webhook' error sometimes.
		*/
		err := utils.WithRetry(10*time.Second, 12, func() error {
			clusterctlClient, err := clusterctlClientLib.New(ctx, "")
			assert.AssertErrNil(ctx, err, "Failed constructing clusterctl client")

			err = clusterctlClient.Move(ctx, clusterctlClientLib.MoveOptions{
				FromKubeconfig: clusterctlClientLib.Kubeconfig{
					Path: constants.OutputPathMainClusterKubeconfig,
				},

				ToKubeconfig: clusterctlClientLib.Kubeconfig{
					Path: kubernetes.GetManagementClusterKubeconfigPath(ctx),
				},

				Namespace: kubernetes.GetCapiClusterNamespace(),
			})
			return err
		})
		assert.AssertErrNil(ctx, err, "Failed reverting pivoting by executing 'clusterctl move'")
	}

	managementClusterClient := kubernetes.MustCreateClusterClient(ctx,
		managementClusterKubeconfigPath,
	)

	// Get the Cluster resource from the management cluster.
	err := kubernetes.GetKubernetesResource(ctx, managementClusterClient, cluster)
	assert.AssertErrNil(ctx, err,
		"Cluster resource was suppossed to be present in the management cluster",
	)

	// If the cluster gets marked as paused, then unmark it first.
	if cluster.Spec.Paused {
		err := managementClusterClient.Update(ctx, cluster)
		assert.AssertErrNil(ctx, err, "Failed unmarking paused cluster")
	}

	// Delete the Cluster resource from the management cluster.
	// This will cause the actual provisioned cluster to be deleted.

	clusterDeletionTimeout := 10 * time.Minute.Milliseconds() // (10 minutes)
	err = managementClusterClient.Delete(ctx, cluster, &client.DeleteOptions{
		GracePeriodSeconds: &clusterDeletionTimeout,
	})
	assert.AssertErrNil(ctx, err, "Failed deleting cluster")

	// Wait for the infrastructure to be destroyed.
	err = wait.PollUntilContextCancel(ctx, 2*time.Minute, false,
		func(ctx context.Context) (bool, error) {
			slog.InfoContext(ctx, "Waiting for cluster infrastructure to be destroyed")

			err := managementClusterClient.Get(ctx,
				types.NamespacedName{
					Name:      cluster.Name,
					Namespace: cluster.Namespace,
				},
				cluster,
			)
			isInfrastructureDeleted := errors.IsNotFound(err)
			return isInfrastructureDeleted, nil
		},
	)
	assert.AssertErrNil(ctx, err, "Failed waiting for the cluster infrastructure to be destroyed")

	slog.InfoContext(ctx, "Deleted cluster successuly")
}
