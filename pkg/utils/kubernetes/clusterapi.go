// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package kubernetes

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	cloudProviderAPI "k8s.io/cloud-provider/api"
	clusterAPIV1Beta1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
)

// Returns whether we're using Clusterapi or not.
func UsingClusterAPI() (usingClusterAPI bool) {
	switch globals.CloudProviderName {
	case constants.CloudProviderBareMetal:
	case constants.CloudProviderLocal:
		usingClusterAPI = false

	default:
		usingClusterAPI = true
	}
	return
}

// Returns the namespace (capi-cluster / capi-cluster-<customer-id>) where the 'cloud-credentials'
// Kubernetes Secret will exist. This Kubernetes Secret will be used by Cluster API to communicate
// with the underlying cloud provider.
func GetCapiClusterNamespace() string {
	capiClusterNamespace := "capi-cluster"
	if config.ParsedGeneralConfig.Obmondo != nil {
		capiClusterNamespace = fmt.Sprintf(
			"capi-cluster-%s",
			config.ParsedGeneralConfig.Obmondo.CustomerID,
		)
	}
	return capiClusterNamespace
}

// Waits for the main cluster to be provisioned.
func WaitForMainClusterToBeProvisioned(ctx context.Context, managementClusterClient client.Client) {
	err := wait.PollUntilContextCancel(ctx, time.Minute, false,
		func(ctx context.Context) (bool, error) {
			slog.InfoContext(ctx, "Waiting for the main cluster to be provisioned")

			// Get the Cluster resource from the management cluster.
			cluster, err := GetClusterResource(ctx, managementClusterClient)
			if err != nil {
				return false, err
			}

			// Cluster phase should be 'Provisioned'.
			if cluster.Status.Phase != string(clusterAPIV1Beta1.ClusterPhaseProvisioned) {
				return false, nil
			}
			//
			// Cluster status should be 'Ready'.
			for _, condition := range cluster.Status.Conditions {
				if condition.Type == clusterAPIV1Beta1.ReadyCondition &&
					condition.Status == "True" {
					return true, nil
				}
			}
			return false, nil
		},
	)
	assert.AssertErrNil(ctx, err, "Failed waiting for the main cluster to be provisioned")
}

// Waits for the main cluster to be ready to run our application workloads.
func WaitForMainClusterToBeReady(ctx context.Context, kubeClient client.Client) {
	for {
		slog.InfoContext(
			ctx,
			"Waiting for the provisioned cluster's Kubernetes API server to be reachable and atleast 1 worker node to be initialized....",
		)

		// List the nodes.
		nodes := &coreV1.NodeList{}
		if err := kubeClient.List(ctx, nodes); err != nil {
			continue
		}

		initializedWorkerNodeCount := 0
		for _, node := range nodes.Items {
			if isControlPlaneNode(&node) {
				continue
			}

			isInitialized := true
			//
			// Check for existence of taints which indicate that the node is uninitialized.
			for _, taint := range node.Spec.Taints {
				if (taint.Key == cloudProviderAPI.TaintExternalCloudProvider) ||
					(taint.Key == clusterAPIV1Beta1.NodeUninitializedTaint.Key) {
					isInitialized = false
				}
			}

			if isInitialized {
				initializedWorkerNodeCount++
			}
		}

		if initializedWorkerNodeCount > 0 {
			return
		}

		time.Sleep(time.Minute)
	}
}

// Saves kubeconfig of the provisioned cluster locally.
func SaveProvisionedClusterKubeconfig(ctx context.Context, kubeClient client.Client) {
	secret := &coreV1.Secret{}
	// Seldom, after the cluster has been provisioned, Cluster API takes some time to create the
	// Kubernetes secret containing the kubeconfig.
	for {
		err := kubeClient.Get(ctx,
			types.NamespacedName{
				Name:      fmt.Sprintf("%s-kubeconfig", config.ParsedGeneralConfig.Cluster.Name),
				Namespace: GetCapiClusterNamespace(),
			},
			secret,
		)
		if err == nil {
			break
		}

		time.Sleep(2 * time.Second)
	}

	kubeConfig := secret.Data["value"]

	err := os.WriteFile(constants.OutputPathMainClusterKubeconfig, kubeConfig, os.ModePerm)
	assert.AssertErrNil(ctx, err, "Failed saving kubeconfig to file")

	slog.InfoContext(ctx, "kubeconfig has been saved locally")
}

// Looks for and returns the Cluster resource in the given Kubernetes cluster.
func GetClusterResource(ctx context.Context,
	clusterClient client.Client,
) (*clusterAPIV1Beta1.Cluster, error) {
	cluster := &clusterAPIV1Beta1.Cluster{
		ObjectMeta: metaV1.ObjectMeta{
			Name:      config.ParsedGeneralConfig.Cluster.Name,
			Namespace: GetCapiClusterNamespace(),
		},
	}

	if err := GetKubernetesResource(ctx, clusterClient, cluster); err != nil {
		return nil, utils.WrapError("Failed getting Cluster resource", err)
	}
	return cluster, nil
}

// Returns whether the 'clusterctl move' command has already been executed or not.
func IsClusterctlMoveExecuted(ctx context.Context) bool {
	mainClusterClient, err := CreateKubernetesClient(ctx,
		constants.OutputPathMainClusterKubeconfig,
	)
	// Main cluster isn't reachable,
	// which means 'clusterctl move' hasn't been executed.
	if err != nil {
		return false
	}

	// If the Cluster resource is found in the main cluster,
	// that means 'clusterctl move' has been executed.
	_, err = GetClusterResource(ctx, mainClusterClient)
	return err == nil
}

// Returns API endpoint of the main cluster, if provisioned.
// Otherwise returns nil.
func GetMainClusterEndpoint(ctx context.Context) *clusterAPIV1Beta1.APIEndpoint {
	kubeConfigPaths := []string{
		GetManagementClusterKubeconfigPath(ctx),
		constants.OutputPathMainClusterKubeconfig,
	}

	for _, kubeConfigPath := range kubeConfigPaths {
		clusterClient, err := CreateKubernetesClient(ctx, kubeConfigPath)
		if err != nil {
			continue
		}

		cluster, err := GetClusterResource(ctx, clusterClient)
		if err == nil {
			return &cluster.Spec.ControlPlaneEndpoint
		}
	}

	return nil
}
