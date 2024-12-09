package utils

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/Obmondo/kubeaid-bootstrap-script/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/utils/logger"
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

	// Create the cluster.
	ExecuteCommandOrDie(fmt.Sprintf(`
		k3d cluster create %s \
			--servers 1 --agents 3 \
			--image rancher/k3s:v1.31.0-k3s1 \
			--wait
	`, name))
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
