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
	clusterAPIV1Beta1 "sigs.k8s.io/cluster-api/api/core/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils"
)

const defaultCapiClusterNamespace = "capi-cluster"

var (
	waitForProvisioningPollInterval = time.Minute
	saveKubeconfigPollInterval      = 2 * time.Second
	outputPathMainClusterKubeconfig = constants.OutputPathMainClusterKubeconfig
)

// Returns whether we're using Clusterapi or not.
func UsingClusterAPI() (usingClusterAPI bool) {
	switch globals.CloudProviderName {
	case constants.CloudProviderBareMetal, constants.CloudProviderLocal:
		usingClusterAPI = false

	default:
		usingClusterAPI = true
	}
	return usingClusterAPI
}

// Returns the namespace (capi-cluster / capi-cluster-<customer-id>) where the 'cloud-credentials'
// Kubernetes Secret will exist. This Kubernetes Secret will be used by Cluster API to communicate
// with the underlying cloud provider.
func GetCapiClusterNamespace() string {
	capiClusterNamespace := defaultCapiClusterNamespace
	if config.ParsedGeneralConfig.Obmondo != nil && config.ParsedGeneralConfig.Obmondo.CustomerID != "" {
		capiClusterNamespace = fmt.Sprintf(
			defaultCapiClusterNamespace+"-%s",
			config.ParsedGeneralConfig.Obmondo.CustomerID,
		)
	}
	return capiClusterNamespace
}

// WaitForMainClusterToBeProvisioned waits for the main cluster to be provisioned.
func WaitForMainClusterToBeProvisioned(ctx context.Context, managementClusterClient client.Client) error {
	err := wait.PollUntilContextCancel(ctx, waitForProvisioningPollInterval, false,
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
	if err != nil {
		return fmt.Errorf("failed waiting for the main cluster to be provisioned: %w", err)
	}
	return nil
}

// WaitForMainClusterToBeReady waits for the main cluster to be ready to run
// application workloads. It polls until at least one initialized worker node
// exists or the context is cancelled.
func WaitForMainClusterToBeReady(ctx context.Context, kubeClient client.Client) error {
	for {
		slog.InfoContext(
			ctx,
			"Waiting for the provisioned cluster's Kubernetes API server to be reachable and atleast 1 worker node to be initialized....",
		)

		// List the nodes.
		nodes := &coreV1.NodeList{}
		if err := kubeClient.List(ctx, nodes); err != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				continue
			}
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
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(waitForProvisioningPollInterval):
		}
	}
}

// SaveProvisionedClusterKubeconfig saves kubeconfig of the provisioned cluster locally.
func SaveProvisionedClusterKubeconfig(ctx context.Context, kubeClient client.Client) error {
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

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(saveKubeconfigPollInterval):
		}
	}

	kubeConfig := secret.Data["value"]

	if err := os.WriteFile(outputPathMainClusterKubeconfig, kubeConfig, 0o600); err != nil {
		return fmt.Errorf("failed saving kubeconfig to file: %w", err)
	}

	slog.InfoContext(ctx, "kubeconfig has been saved locally")
	return nil
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
	mainClusterClient, err := createKubernetesClientFn(ctx,
		outputPathMainClusterKubeconfig,
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
