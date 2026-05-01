// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package kubernetes

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"

	caphV1Beta1 "github.com/syself/cluster-api-provider-hetzner/api/v1beta1"
	veleroV1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	appsV1 "k8s.io/api/apps/v1"
	batchV1 "k8s.io/api/batch/v1"
	coreV1 "k8s.io/api/core/v1"
	k8sAPIErrors "k8s.io/apimachinery/pkg/api/errors"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	kubeadmConstants "k8s.io/kubernetes/cmd/kubeadm/app/constants"
	capaV1Beta2 "sigs.k8s.io/cluster-api-provider-aws/v2/api/v1beta2"
	capzV1Beta1 "sigs.k8s.io/cluster-api-provider-azure/api/v1beta1"
	kcpV1Beta1 "sigs.k8s.io/cluster-api/api/controlplane/kubeadm/v1beta1"
	clusterAPIV1Beta1 "sigs.k8s.io/cluster-api/api/core/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/commandexecutor"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
)

var (
	amContainerizedFn        = amContainerized
	loadKubeConfigFromFileFn = clientcmd.LoadFromFile
	createKubernetesClientFn = CreateKubernetesClient
	pingKubernetesClusterFn  = pingKubernetesCluster
	dockerEnvPathFn          = func() string { return "/.dockerenv" }
	newClientFn              = client.New
)

// Returns the management cluster kubeconfig file path, based on whether the script is running
// inside a container or not.
func GetManagementClusterKubeconfigPath(ctx context.Context) (string, error) {
	containerized, err := amContainerizedFn()
	if err != nil {
		return "", fmt.Errorf("failed determining management cluster kubeconfig path: %w", err)
	}

	if containerized {
		return constants.OutputPathManagementClusterContainerKubeconfig, nil
	}

	return constants.OutputPathManagementClusterHostKubeconfig, nil
}

// Detects whether the KubeAid Bootstrap Script is running inside a container or not.
// If the /.dockerenv file exists, then that means, it's running inside a container.
// Only compatible with the Docker container engine for now.
func amContainerized() (bool, error) {
	_, err := os.Stat(dockerEnvPathFn())
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed detecting whether running inside a container: %w", err)
	}
	return true, nil
}

// Tries to create a Kubernetes Go client using the Kubeconfig file present at the given path.
// Returns the Kubernetes Go client.
func CreateKubernetesClient(ctx context.Context, kubeconfigPath string) (client.Client, error) {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("kubeconfig", kubeconfigPath),
	})

	kubeconfig, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("failed building config from kubeconfig file: %w", err)
	}

	scheme := runtime.NewScheme()

	for _, addScheme := range []struct {
		name string
		fn   func(*runtime.Scheme) error
	}{
		{"Core v1", coreV1.AddToScheme},
		{"Apps v1", appsV1.AddToScheme},
		{"Batch v1", batchV1.AddToScheme},
		{"ClusterAPI v1beta1", clusterAPIV1Beta1.AddToScheme},
		{"KCP (Kubeadm Control plane Provider) v1beta1", kcpV1Beta1.AddToScheme},
		{"CAPA (ClusterAPI Provider AWS) v1beta2", capaV1Beta2.AddToScheme},
		{"CAPZ (ClusterAPI Provider Azure) v1beta1", capzV1Beta1.AddToScheme},
		{"CAPH (ClusterAPI Provider Hetzner) v1beta1", caphV1Beta1.AddToScheme},
		{"Velero v1", veleroV1.AddToScheme},
	} {
		if err := addScheme.fn(scheme); err != nil {
			return nil, fmt.Errorf("failed adding %s scheme: %w", addScheme.name, err)
		}
	}

	clusterClient, err := newClientFn(kubeconfig, client.Options{
		Scheme: scheme,
	})
	if err != nil {
		return nil, fmt.Errorf("failed creating kube client from kubeconfig: %w", err)
	}

	err = pingKubernetesClusterFn(ctx, clusterClient)
	return clusterClient, err
}

func pingKubernetesCluster(ctx context.Context, clusterClient client.Client) error {
	deployments := &appsV1.DeploymentList{}
	err := clusterClient.List(ctx, deployments, &client.ListOptions{
		Namespace: "default",
	})
	if err != nil {
		return fmt.Errorf("failed pinging the kubernetes cluster by listing deployments in the default namespace: %w", err)
	}
	return nil
}

func GetMainClusterEndpoint(ctx context.Context) (*url.URL, error) {
	kubeConfig, err := loadKubeConfigFromFileFn(constants.OutputPathMainClusterKubeconfig)
	if errors.Is(err, os.ErrNotExist) {
		// The kubeconfig file doesn't exist,
		// which means the main cluster hasn't been provisioned yet.
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed reading main cluster's kubeconfig file: %w", err)
	}

	mainCluster, ok := kubeConfig.Clusters[config.ParsedGeneralConfig.Cluster.Name]
	if !ok {
		return nil, nil
	}

	endpoint, err := url.Parse(mainCluster.Server)
	if err != nil {
		return nil, fmt.Errorf("failed parsing main cluster's API server endpoint %q: %w", mainCluster.Server, err)
	}

	// Ping the K8s API server once.

	clusterClient, err := createKubernetesClientFn(ctx, constants.OutputPathMainClusterKubeconfig)
	if err != nil {
		return nil, nil
	}

	err = pingKubernetesClusterFn(ctx, clusterClient)
	if err != nil {
		return nil, nil
	}

	return endpoint, nil
}

// Creates the given namespace (if it doesn't already exist).
func CreateNamespace(ctx context.Context, namespaceName string, clusterClient client.Client) error {
	namespace := &coreV1.Namespace{
		ObjectMeta: metaV1.ObjectMeta{
			Name: namespaceName,
		},
	}

	err := clusterClient.Create(ctx, namespace)
	if k8sAPIErrors.IsAlreadyExists(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed creating namespace %q: %w", namespaceName, err)
	}
	return nil
}

// Tries to fetch the given Kubernetes resource using the given Kubernetes cluster client.
func GetKubernetesResource(ctx context.Context, clusterClient client.Client, resource client.Object) error {
	return clusterClient.Get(ctx,
		types.NamespacedName{
			Name:      resource.GetName(),
			Namespace: resource.GetNamespace(),
		},
		resource,
	)
}

// Returns whether the given node object is part of the control plane or not.
func isControlPlaneNode(node *coreV1.Node) bool {
	for key := range node.Labels {
		if key == kubeadmConstants.LabelNodeRoleControlPlane {
			return true
		}
	}
	return false
}

// Triggers the given CRONJob, by creating a Job from its Job template.
func TriggerCRONJob(ctx context.Context, objectKey client.ObjectKey, clusterClient client.Client) error {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("cronjob", objectKey.Name),
		slog.String("namespace", objectKey.Namespace),
	})

	// Get the CRONJob.
	cronJob := batchV1.CronJob{
		ObjectMeta: metaV1.ObjectMeta{
			Name:      objectKey.Name,
			Namespace: objectKey.Namespace,
		},
	}
	if err := GetKubernetesResource(ctx, clusterClient, &cronJob); err != nil {
		return fmt.Errorf("failed getting CRONJob %q: %w", objectKey.Name, err)
	}

	// Create a Job using the CRONJob's Job template.
	job := &batchV1.Job{
		ObjectMeta: metaV1.ObjectMeta{
			GenerateName: objectKey.Name,
			Namespace:    objectKey.Namespace,
		},

		Spec: cronJob.Spec.JobTemplate.Spec,
	}
	if err := clusterClient.Create(ctx, job, &client.CreateOptions{}); err != nil {
		return fmt.Errorf("failed creating Job from CRONJob %q: %w", objectKey.Name, err)
	}

	slog.InfoContext(ctx, "Triggered CRONJob", slog.String("job", job.Name))
	return nil
}

// Returns whether there are zero node-groups or not.
// If yes, then we need to remove taints from the control-plane nodes right after the main cluster
// is provisioned.
func IsNodeGroupCountZero(ctx context.Context) bool {
	switch globals.CloudProviderName {
	case constants.CloudProviderAWS:
		return len(config.ParsedGeneralConfig.Cloud.AWS.NodeGroups) == 0

	case constants.CloudProviderAzure:
		return len(config.ParsedGeneralConfig.Cloud.Azure.NodeGroups) == 0

	case constants.CloudProviderHetzner:
		nodeGroups := config.ParsedGeneralConfig.Cloud.Hetzner.NodeGroups
		return (len(nodeGroups.HCloud) + len(nodeGroups.BareMetal)) == 0

	case constants.CloudProviderBareMetal:
		return len(config.ParsedGeneralConfig.Cloud.BareMetal.NodeGroups) == 0
	}

	return false
}

// RemoveNoScheduleTaintsFromMasterNodes removes the
// 'node-role.kubernetes.io/control-plane:NoSchedule' taint from master nodes.
func RemoveNoScheduleTaintsFromMasterNodes(ctx context.Context, clusterClient client.Client) error {
	return removeNoScheduleTaintsFromMasterNodes(ctx, clusterClient, commandexecutor.NewLocalCommandExecutor(false))
}

func removeNoScheduleTaintsFromMasterNodes(ctx context.Context, clusterClient client.Client, exec commandexecutor.CommandExecutor) error {
	slog.InfoContext(ctx, "Removing no-schedule taints from master nodes")

	var masterNodeList coreV1.NodeList
	err := clusterClient.List(ctx, &masterNodeList, client.MatchingLabels{
		kubeadmConstants.LabelNodeRoleControlPlane: "",
	})
	if err != nil {
		return fmt.Errorf("failed listing master nodes: %w", err)
	}

	for _, masterNode := range masterNodeList.Items {
		for _, taint := range masterNode.Spec.Taints {
			// We only remove the taint whose key matches the control-plane role label;
			// the effect is always NoSchedule by kubeadm convention.
			if taint.Key == kubeadmConstants.LabelNodeRoleControlPlane {
				exec.MustExecute(ctx,
					fmt.Sprintf(
						"kubectl taint node %s node-role.kubernetes.io/control-plane:NoSchedule-",
						masterNode.Name,
					),
				)
			}
		}
	}
	return nil
}
