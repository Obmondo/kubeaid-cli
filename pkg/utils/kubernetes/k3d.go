package kubernetes

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
	k3dClient "github.com/k3d-io/k3d/v5/pkg/client"
	"github.com/k3d-io/k3d/v5/pkg/runtimes"
)

// Creates a K3D cluster with the given name (only if it doesn't already exist).
func CreateK3DCluster(ctx context.Context, name string) {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("cluster", name),
	})

	// Skip creating the cluster, if it already exists.
	if doesK3dClusterExist(ctx, name) {
		slog.InfoContext(ctx, "Skipped creating the K3d management cluster")
		return
	}

	slog.InfoContext(ctx, "Creating the K3d management cluster")

	// Kubernetes API Server host for MacOS.
	// NOTE : The user must make sure, that host.docker.internal is mapped to 127.0.0.1 in the host
	//        machine.
	apiServerHost := "host.docker.internal"

	// In case of Linux, the Kubernetes API Server host is set to the Docker Bridge Gateway IP.
	if runtime.GOOS == "linux" {
		apiServerHost = "172.17.0.1"
	}

	// Create the cluster.
	utils.ExecuteCommandOrDie(fmt.Sprintf(`
		k3d cluster create %s \
			--servers 1 --agents 3 \
			--image rancher/k3s:v1.31.0-k3s1 \
      --api-port %s:6445 \
			--wait
	`, name, apiServerHost))

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
