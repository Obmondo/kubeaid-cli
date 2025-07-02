package core

import (
	"context"
	"log/slog"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/kubernetes"
)

func TestCluster(ctx context.Context) {
	// Set the KUBECONFIG environment variable to the main cluster's kubeconfig.
	utils.MustSetEnv(constants.EnvNameKubeconfig, constants.OutputPathMainClusterKubeconfig)

	// Construct a client to the main cluster.
	mainClusterClient := kubernetes.MustCreateClusterClient(ctx,
		constants.OutputPathMainClusterKubeconfig,
	)

	// Run minimal Cilium network connectivity tests.
	runCiliumNetworkConnectivityTests(ctx, mainClusterClient)
}

func runCiliumNetworkConnectivityTests(ctx context.Context, clusterClient client.Client) {
	slog.InfoContext(ctx, "🧪 Running minimal Cilium network connectivity tests")

	// Create the cilium-test namespace.
	kubernetes.CreateNamespace(ctx, "cilium-test", clusterClient)
	//
	// Pods spun up during the network connectivity tests, need to do DNS lookups and tcpdumps.
	// So they need to run in privileged mode.
	// Let's apply appropriate namespace label, to enforce the privileged Pod Security Standard.
	// REFER : https://kubernetes.io/docs/tasks/configure-pod-container/enforce-standards-namespace-labels/.
	utils.ExecuteCommandOrDie(
		"kubectl label namespace cilium-test pod-security.kubernetes.io/enforce=privileged",
	)

	// Run minimal Cilium network connectivity tests.
	_, err := utils.ExecuteCommand(`
    cilium connectivity test \
      --namespace cilium \
      --test-namespace cilium-test \
      --test "!" \
      --timeout 5m
  `)
	if err != nil {
		slog.ErrorContext(ctx, "🚨 Cilium network connectivity tests failed")
	} else {
		slog.InfoContext(ctx, "✅ Cilium connectivity tests passed")
	}

	// Cleanup resources created during the Cilium network connectivity tests.
	utils.ExecuteCommandOrDie("kubectl delete namespace cilium-test cilium-test-1")
}
