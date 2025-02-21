package kubernetes

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
	appsV1 "k8s.io/api/apps/v1"
	coreV1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/clientcmd"
	kubeadmConstants "k8s.io/kubernetes/cmd/kubeadm/app/constants"
	capaV1Beta2 "sigs.k8s.io/cluster-api-provider-aws/v2/api/v1beta2"
	clusterAPIV1Beta1 "sigs.k8s.io/cluster-api/api/v1beta1"
	kcpV1Beta1 "sigs.k8s.io/cluster-api/controlplane/kubeadm/api/v1beta1" // KCP = Kubeadm Control plane Provider.
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Uses the kubeconfig file present at the given path, to create and return a Kubernetes Go client.
func CreateKubernetesClient(ctx context.Context,
	kubeconfigPath string,
	panicOnKubeconfigBuildFailure bool,
) (client.Client, error) {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("kubeconfig", kubeconfigPath),
	})

	kubeconfig, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return nil, utils.WrapError("Failed building config from kubeconfig file", err)
	}

	scheme := runtime.NewScheme()

	err = coreV1.AddToScheme(scheme)
	assert.AssertErrNil(ctx, err, "Failed adding Core v1 scheme")

	err = appsV1.AddToScheme(scheme)
	assert.AssertErrNil(ctx, err, "Failed adding Apps v1 scheme")

	err = clusterAPIV1Beta1.AddToScheme(scheme)
	assert.AssertErrNil(ctx, err, "Failed adding ClusterAPI v1beta1 scheme")

	err = kcpV1Beta1.AddToScheme(scheme)
	assert.AssertErrNil(ctx, err, "Failed adding KCP (Kubeadm Control plane Providerr) v1beta1 scheme")

	err = capaV1Beta2.AddToScheme(scheme)
	assert.AssertErrNil(ctx, err, "Failed adding CAPA (ClusterAPI Provider AWS) v1beta1 scheme")

	kubeClient, err := client.New(kubeconfig, client.Options{
		Scheme: scheme,
	})
	assert.AssertErrNil(ctx, err, "Failed creating kube client from kubeconfig")

	err = pingKubernetesCluster(ctx, kubeClient)
	return kubeClient, err
}

// Checks whether the Kubernetes cluster is reachable or not, by trying to list the Deployments in
// the default namespace.
func pingKubernetesCluster(ctx context.Context, kubeClient client.Client) error {
	deployments := &appsV1.DeploymentList{}
	err := kubeClient.List(ctx, deployments, &client.ListOptions{
		Namespace: "default",
	})
	if err != nil {
		return fmt.Errorf("Failed pinging the Kubernetes cluster by trying to list Deployments in the default namespace : %v", err)
	}
	return nil
}

// Returns the namespace (capi-cluster / capi-cluster-<customer-id>) where the 'cloud-credentials'
// Kubernetes Secret will exist. This Kubernetes Secret will be used by Cluster API to communicate
// with the underlying cloud provider.
func GetCapiClusterNamespace() string {
	capiClusterNamespace := "capi-cluster"
	if len(config.ParsedConfig.CustomerID) > 0 {
		capiClusterNamespace = fmt.Sprintf("capi-cluster-%s", config.ParsedConfig.CustomerID)
	}
	return capiClusterNamespace
}

// Creates the given namespace (if it doesn't already exist).
func CreateNamespace(ctx context.Context, namespaceName string, kubeClient client.Client) {
	namespace := &coreV1.Namespace{
		ObjectMeta: metaV1.ObjectMeta{
			Name: namespaceName,
		},
	}

	err := kubeClient.Create(ctx, namespace)
	if errors.IsAlreadyExists(err) {
		return
	}
	assert.AssertErrNil(ctx, err, "Failed creating namespace", slog.String("namespace", namespaceName))
}

// Installs Sealed Secrets in the underlying Kubernetes cluster.
func InstallSealedSecrets(ctx context.Context) {
	HelmInstall(ctx, &HelmInstallArgs{
		RepoName:    "sealed-secrets",
		RepoURL:     "https://bitnami-labs.github.io/sealed-secrets/",
		ChartName:   "sealed-secrets",
		Version:     "2.17.1",
		Namespace:   "sealed-secrets",
		ReleaseName: "sealed-secrets",
		Values:      "fullnameOverride=sealed-secrets-controller",
	})
}

// Takes the path to a Kubernetes Secret file. It replaces the contents of that file by generating
// the corresponding Sealed Secret.
func GenerateSealedSecret(ctx context.Context, secretFilePath string) {
	utils.ExecuteCommandOrDie(fmt.Sprintf(`
		kubeseal \
			--controller-name sealed-secrets-controller --controller-namespace sealed-secrets \
			--secret-file %s --sealed-secret-file %s
	`, secretFilePath, secretFilePath))
}

// Waits for the main cluster to be provisioned.
func WaitForMainClusterToBeProvisioned(ctx context.Context, kubeClient client.Client) {
	wait.PollUntilContextCancel(ctx, time.Minute, false, func(ctx context.Context) (bool, error) {
		slog.Info("Waiting for the main cluster to be provisioned")

		// Get the Cluster resource from the management cluster.
		cluster := &clusterAPIV1Beta1.Cluster{}
		if err := GetKubernetesResource(ctx, kubeClient, cluster); err != nil {
			return false, err
		}

		// Cluster phase should be 'Provisioned'.
		if cluster.Status.Phase != string(clusterAPIV1Beta1.ClusterPhaseProvisioned) {
			return false, nil
		}
		//
		// Cluster status should be 'Ready'.
		for _, condition := range cluster.Status.Conditions {
			if condition.Type == clusterAPIV1Beta1.ReadyCondition && condition.Status == "True" {
				return true, nil
			}
		}
		return false, nil
	})
}

// Tries to fetch the given Kubernetes resource using the given Kubernetes cluster client.
func GetKubernetesResource(ctx context.Context, kubeClient client.Client, resource client.Object) error {
	return kubeClient.Get(ctx,
		types.NamespacedName{
			Name:      resource.GetName(),
			Namespace: resource.GetNamespace(),
		},
		resource,
	)
}

// Waits for the main cluster to be ready to run our application workloads.
func WaitForMainClusterToBeReady(ctx context.Context, kubeClient client.Client) {
	wait.PollUntilContextCancel(ctx, time.Minute, false, func(ctx context.Context) (bool, error) {
		slog.Info("Waiting for the provisioned cluster's Kubernetes API server to be reachable and atleast 1 worker node to be initialized....")

		// List the nodes.
		nodes := &coreV1.NodeList{}
		if err := kubeClient.List(ctx, nodes); err != nil {
			return false, err
		}

		initializedWorkerNodeCount := 0
		for _, node := range nodes.Items {
			if isControlPlaneNode(&node) {
				continue
			}

			isUninitialized := false
			//
			// Check whether the 'node.cluster.x-k8s.io/uninitialized' taint exists for the node or not.
			// If yes, that means the node is still uninitialized.
			for _, taint := range node.Spec.Taints {
				if taint.Key == clusterAPIV1Beta1.NodeUninitializedTaint.Key {
					isUninitialized = true
				}
			}

			if !isUninitialized {
				initializedWorkerNodeCount++
			}
		}
		isClusterReady := (initializedWorkerNodeCount > 0)
		return isClusterReady, nil
	})
}

// Returns whether the given node object is part of the control plane or not.
func isControlPlaneNode(node *coreV1.Node) bool {
	isControlPlaneNode := false
	for key := range node.Labels {
		if key == kubeadmConstants.LabelNodeRoleControlPlane {
			isControlPlaneNode = true
		}
	}
	return isControlPlaneNode
}

// Saves kubeconfig of the provisioned cluster locally.
func SaveKubeconfig(ctx context.Context, kubeClient client.Client) {
	secret := &coreV1.Secret{}
	// Seldom, after the cluster has been provisioned, Cluster API takes some time to create the
	// Kubernetes secret containing the kubeconfig.
	for {
		err := kubeClient.Get(ctx,
			types.NamespacedName{
				Name:      fmt.Sprintf("%s-kubeconfig", config.ParsedConfig.Cluster.Name),
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

	err := os.WriteFile(constants.OutputPathProvisionedClusterKubeconfig, kubeConfig, 0644)
	assert.AssertErrNil(ctx, err, "Failed saving kubeconfig to file")

	slog.InfoContext(ctx, "kubeconfig has been saved locally")
}

// Returns whether the `clusterctl move` command has already been executed or not.
func IsClusterctlMoveExecuted(ctx context.Context, provisionedClusterClient client.Client) bool {
	// If the Cluster resource is found in the provisioned cluster, that means `clusterctl move` has
	// been executed.
	err := GetKubernetesResource(ctx, provisionedClusterClient, &clusterAPIV1Beta1.Cluster{
		ObjectMeta: metaV1.ObjectMeta{
			Name:      config.ParsedConfig.Cluster.Name,
			Namespace: GetCapiClusterNamespace(),
		},
	})
	return err == nil
}
