// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package core

import (
	"context"
	"log/slog"
	"os"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/commandexecutor"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/kubernetes"
)

func TestCluster(ctx context.Context) {
	// Ensure that required runtime dependencies are installed.
	if err := utils.EnsureRuntimeDependencyInstalled("cilium-cli"); err != nil {
		slog.ErrorContext(ctx, "Runtime dependency unavailable", slog.String("error", err.Error()))
		os.Exit(1)
	}

	// Set the KUBECONFIG environment variable to the main cluster's kubeconfig.
	utils.MustSetEnv(constants.EnvNameKubeconfig, constants.OutputPathMainClusterKubeconfig)

	// Construct a client to the main cluster.
	mainClusterClient, err := kubernetes.CreateKubernetesClient(ctx,
		constants.OutputPathMainClusterKubeconfig,
	)
	assert.AssertErrNil(ctx, err, "Failed constructing Kubernetes cluster client")

	// Run minimal Cilium network connectivity tests.
	runCiliumNetworkConnectivityTests(ctx, mainClusterClient)
}

func runCiliumNetworkConnectivityTests(ctx context.Context, clusterClient client.Client) {
	slog.InfoContext(ctx, "🧪 Running minimal Cilium network connectivity tests")

	// Create the cilium-test namespace.
	err := kubernetes.CreateNamespace(ctx, constants.NamespaceCiliumTest, clusterClient)
	assert.AssertErrNil(ctx, err, "Failed creating namespace",
		slog.String("namespace", constants.NamespaceCiliumTest))
	//
	// Pods spun up during the network connectivity tests, need to do DNS lookups and tcpdumps.
	// So they need to run in privileged mode.
	// Let's apply appropriate namespace label, to enforce the privileged Pod Security Standard.
	// REFER : https://kubernetes.io/docs/tasks/configure-pod-container/enforce-standards-namespace-labels/.
	commandexecutor.NewLocalCommandExecutor(false).MustExecute(ctx,
		"kubectl label namespace cilium-test pod-security.kubernetes.io/enforce=privileged")

	// Run minimal Cilium network connectivity tests.
	commandexecutor.NewLocalCommandExecutor(true).MustExecute(ctx, `
    cilium-cli connectivity test \
      --namespace cilium \
      --test-namespace cilium-test \
      --test ! \
      --timeout 5m
  `)
	slog.InfoContext(ctx, "✅ Cilium connectivity tests passed")

	// Cleanup resources created during the Cilium network connectivity tests.
	commandexecutor.NewLocalCommandExecutor(false).MustExecute(ctx,
		"kubectl delete namespace cilium-test cilium-test-1")
}
