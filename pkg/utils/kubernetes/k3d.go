package kubernetes

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
	k3dClient "github.com/k3d-io/k3d/v5/pkg/client"
	"github.com/k3d-io/k3d/v5/pkg/runtimes"
)

// Creates a K3D cluster with the given name, if it doesn't already exist.
//
// The user needs to create a Docker Network (preferably named `k3d-management-cluster`) and run
// the KubeAid Bootstrap Script container in that Docker Network.
// The K3D cluster will reuse that existing network.
// From inside the container, we can access the K3D cluster's API server using
// https://k3d-management-cluster-server-0:6443.
// And from outside the container, we can use https://0.0.0.0:<whatever the random port is>.
func CreateK3DCluster(ctx context.Context, name string) {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("cluster-name", name),
	})

	// Create the K3D cluster if it doesn't already exist.
	if !doesK3dClusterExist(ctx, name) {
		slog.InfoContext(ctx, "Creating the K3d management cluster")

		// We'll reuse the existing k3d-management-cluster Docker Network.
		utils.ExecuteCommandOrDie(fmt.Sprintf(
			`
        k3d cluster create %s \
          --servers 1 --agents 3 \
          --image rancher/k3s:%s-k3s1 \
          --k3s-arg "--tls-san=0.0.0.0@server:*" \
          --network k3d-%s \
          --wait
			`,
			name,
			config.ParsedConfig.Cluster.K8sVersion,
			name,
		))
	} else {
		slog.InfoContext(ctx, "Skipped creating the K3d management cluster")
	}

	// Create the management cluster's host kubeconfig.
	// Use https://0.0.0.0:<whatever the random port is> as the API server address.
	utils.ExecuteCommandOrDie(fmt.Sprintf(
		"k3d kubeconfig get %s > %s",
		name,
		constants.OutputPathManagementClusterHostKubeconfig,
	))

	// For management cluster's in-container kubeconfig, use
	// https://k3d-management-cluster-server-0:6443 as the API server address.
	utils.ExecuteCommandOrDie(fmt.Sprintf(
		"cp %s %s && KUBECONFIG=%s kubectl config set-cluster k3d-%s --server=https://k3d-%s-server-0:6443",
		constants.OutputPathManagementClusterHostKubeconfig,
		constants.OutputPathManagementClusterContainerKubeconfig,
		constants.OutputPathManagementClusterContainerKubeconfig,
		name,
		name,
	))

	// Initially the master nodes have label node-role.kubernetes.io/control-plane set to "true".
	// We'll change the label value to "" (just like it is in Vanilla Kubernetes).
	// Some apps (like capi-cluster) relies on this label to get scheduled to the master node.
	utils.ExecuteCommandOrDie(fmt.Sprintf(`
		master_nodes=$(kubectl get nodes -l node-role.kubernetes.io/control-plane=true -o name)

		for node in $master_nodes; do
			kubectl label $node node-role.kubernetes.io/control-plane-
			kubectl label $node node-role.kubernetes.io/control-plane=""
		done
	`))
}

// Returns whether the given K3d cluster exists or not.
func doesK3dClusterExist(ctx context.Context, name string) bool {
	clusters, err := k3dClient.ClusterList(ctx, runtimes.Docker)
	assert.AssertErrNil(ctx, err, "Failed listing K3d clusters")

	for _, cluster := range clusters {
		if cluster.Name == name {
			return true
		}
	}
	return false
}
