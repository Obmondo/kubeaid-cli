package kubernetes

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
	k3dClient "github.com/k3d-io/k3d/v5/pkg/client"
	"github.com/k3d-io/k3d/v5/pkg/runtimes"
)

// Creates a K3D cluster with the given name (only if it doesn't already exist).
//
// The user needs to create a Docker Network (preferably named `k3d-management-cluster`) and run
// the KubeAid Bootstrap Script container in that Docker Network.
// The K3D cluster will reuse that existing network.
// From inside the container, we can access the K3D cluster's API server using
// https://k3d-management-cluster-server-0:6443.
// And from outside the container, we can use https://127.0.0.1:<whatever the random port is>.
func CreateK3DCluster(ctx context.Context, name string) {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("cluster", name),
	})

	// Create the K3D cluster if it doesn't already exist.
	if !doesK3dClusterExist(ctx, name) {
		slog.InfoContext(ctx, "Creating the K3d management cluster")

		// We'll reuse the existing k3d-management-cluster Docker Network.
		utils.ExecuteCommandOrDie(fmt.Sprintf(
			`
        k3d cluster create %s \
          --servers 1 --agents 3 \
          --image rancher/k3s:v1.31.0-k3s1 \
          --network k3d-management-cluster \
          --wait
	    `,
			name,
		))
	} else {
		slog.InfoContext(ctx, "Skipped creating the K3d management cluster")
	}

	// Create the management cluster's host kubeconfig.
	// Use https://127.0.0.1:<whatever the random port is> as the API server address.
	utils.ExecuteCommandOrDie(fmt.Sprintf(
		`
      cp %s %s && \
        KUBECONFIG=%s kubectl config set-cluster k3d-management-cluster --server=$(kubectl config view --minify -o jsonpath='{.clusters[?(@.name=="k3d-management-cluster")].cluster.server}' | sed 's#https://[^:]*#https://127.0.0.1#')
    `,
		constants.OutputPathManagementClusterContainerKubeconfig,
		constants.OutputPathManagementClusterHostKubeconfig,
		constants.OutputPathManagementClusterHostKubeconfig,
	))

	// For management cluster's in-container kubeconfig, use
	// https://k3d-management-cluster-server-0:6443 as the API server address.
	utils.ExecuteCommandOrDie(fmt.Sprintf(
		"KUBECONFIG=%s kubectl config set-cluster k3d-management-cluster --server=https://k3d-management-cluster-server-0:6443",
		constants.OutputPathManagementClusterContainerKubeconfig,
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
