package kubernetes

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path"

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
	clusterAPIV1Beta1 "sigs.k8s.io/cluster-api/api/v1beta1"
	kcpV1Beta1 "sigs.k8s.io/cluster-api/controlplane/kubeadm/api/v1beta1" // KCP = Kubeadm Control plane Provider.
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
)

// Returns the management cluster kubeconfig file path, based on whether the script is running
// inside a container or not.
func GetManagementClusterKubeconfigPath(ctx context.Context) string {
	if amContainerized(ctx) {
		return constants.OutputPathManagementClusterContainerKubeconfig
	}

	return constants.OutputPathManagementClusterHostKubeconfig
}

// Detetcs whether the KubeAid Bootstrap Script is running inside a container or not.
// If the /.dockerenv file exists, then that means, it's running inside a container.
// Only compatible with the Docker container engine for now.
func amContainerized(ctx context.Context) bool {
	_, err := os.Stat("/.dockerenv")
	if errors.Is(err, os.ErrNotExist) {
		return false
	}

	assert.AssertErrNil(ctx, err, "Failed detecting whether running inside a container or not")
	return true
}

// Creates a Kubernetes Go client using the Kubeconfig file present at the given path.
func MustCreateClusterClient(ctx context.Context, kubeconfigPath string) client.Client {
	clusterClient, err := CreateKubernetesClient(ctx, kubeconfigPath)
	assert.AssertErrNil(ctx, err,
		"Failed constructing Kubernetes cluster client",
		slog.String("kubeconfig", kubeconfigPath),
	)

	return clusterClient
}

// Tries to create a Kubernetes Go client using the Kubeconfig file present at the given path.
// Returns the Kubernetes Go client.
func CreateKubernetesClient(ctx context.Context, kubeconfigPath string) (client.Client, error) {
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

	err = batchV1.AddToScheme(scheme)
	assert.AssertErrNil(ctx, err, "Failed adding Batch v1 scheme")

	err = clusterAPIV1Beta1.AddToScheme(scheme)
	assert.AssertErrNil(ctx, err, "Failed adding ClusterAPI v1beta1 scheme")

	err = kcpV1Beta1.AddToScheme(scheme)
	assert.AssertErrNil(ctx, err,
		"Failed adding KCP (Kubeadm Control plane Providerr) v1beta1 scheme",
	)

	err = capaV1Beta2.AddToScheme(scheme)
	assert.AssertErrNil(ctx, err, "Failed adding CAPA (ClusterAPI Provider AWS) v1beta2 scheme")

	err = capzV1Beta1.AddToScheme(scheme)
	assert.AssertErrNil(ctx, err, "Failed adding CAPZ (ClusterAPI Provider Azure) v1beta1 scheme")

	err = caphV1Beta1.AddToScheme(scheme)
	assert.AssertErrNil(ctx, err, "Failed adding CAPH (ClusterAPI Provider Hetzner) v1beta1 scheme")

	err = veleroV1.AddToScheme(scheme)
	assert.AssertErrNil(ctx, err, "Failed adding Velero v1 scheme")

	clusterClient, err := client.New(kubeconfig, client.Options{
		Scheme: scheme,
	})
	assert.AssertErrNil(ctx, err, "Failed creating kube client from kubeconfig")

	err = pingKubernetesCluster(ctx, clusterClient)
	return clusterClient, err
}

// Checks whether the Kubernetes cluster is reachable or not, by trying to list the Deployments in
// the default namespace.
func pingKubernetesCluster(ctx context.Context, clusterClient client.Client) error {
	deployments := &appsV1.DeploymentList{}
	err := clusterClient.List(ctx, deployments, &client.ListOptions{
		Namespace: "default",
	})
	if err != nil {
		return utils.WrapError(
			"Failed pinging the Kubernetes cluster by trying to list Deployments in the default namespace : %w",
			err,
		)
	}
	return nil
}

// Returns the namespace (capi-cluster / capi-cluster-<customer-id>) where the 'cloud-credentials'
// Kubernetes Secret will exist. This Kubernetes Secret will be used by Cluster API to communicate
// with the underlying cloud provider.
func GetCapiClusterNamespace() string {
	capiClusterNamespace := "capi-cluster"
	if len(config.ParsedGeneralConfig.Obmondo.CustomerID) > 0 {
		capiClusterNamespace = fmt.Sprintf(
			"capi-cluster-%s",
			config.ParsedGeneralConfig.Obmondo.CustomerID,
		)
	}
	return capiClusterNamespace
}

// Creates the given namespace (if it doesn't already exist).
func CreateNamespace(ctx context.Context, namespaceName string, clusterClient client.Client) {
	namespace := &coreV1.Namespace{
		ObjectMeta: metaV1.ObjectMeta{
			Name: namespaceName,
		},
	}

	err := clusterClient.Create(ctx, namespace)
	if k8sAPIErrors.IsAlreadyExists(err) {
		return
	}
	assert.AssertErrNil(ctx, err,
		"Failed creating namespace",
		slog.String("namespace", namespaceName),
	)
}

// Installs Sealed Secrets in the underlying Kubernetes cluster.
func InstallSealedSecrets(ctx context.Context) {
	HelmInstall(ctx, &HelmInstallArgs{
		ChartPath:   path.Join(utils.GetKubeAidDir(), "argocd-helm-charts/sealed-secrets"),
		Namespace:   "sealed-secrets",
		ReleaseName: "sealed-secrets",
		Values: map[string]any{
			"sealed-secrets": map[string]any{
				"namespace":        "sealed-secrets",
				"fullnameOverride": "sealed-secrets-controller",
			},
			"backup": map[string]any{},
		},
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

// Tries to fetch the given Kubernetes resource using the given Kubernetes cluster client.
func GetKubernetesResource(ctx context.Context,
	clusterClient client.Client,
	resource client.Object,
) error {
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
func TriggerCRONJob(ctx context.Context, objectKey client.ObjectKey, clusterClient client.Client) {
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
	err := GetKubernetesResource(ctx, clusterClient, &cronJob)
	assert.AssertErrNil(ctx, err, "Failed getting CRONJob")

	// Create a Job using the CRONJob's Job template.
	job := &batchV1.Job{
		ObjectMeta: metaV1.ObjectMeta{
			GenerateName: objectKey.Name,
			Namespace:    objectKey.Namespace,
		},

		Spec: cronJob.Spec.JobTemplate.Spec,
	}
	err = clusterClient.Create(ctx, job, &client.CreateOptions{})
	assert.AssertErrNil(ctx, err, "Failed creating Job", slog.String("job", job.Name))

	slog.InfoContext(ctx, "Triggered CRONJob", slog.String("job", job.Name))
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
	}

	return false
}

// Removes the 'node-role.kubernetes.io/control-plane:NoSchedule' taint from master nodes.
func RemoveNoScheduleTaintsFromMasterNodes(ctx context.Context, clusterClient client.Client) {
	slog.InfoContext(ctx, "Removing no-schedule taints from master nodes")

	// List the master nodes.
	var masterNodeList coreV1.NodeList
	err := clusterClient.List(ctx, &masterNodeList, client.MatchingLabels{
		kubeadmConstants.LabelNodeRoleControlPlane: "",
	})
	assert.AssertErrNil(ctx, err, "Failed listing master nodes")

	// For each master node.
	for _, masterNode := range masterNodeList.Items {
		for _, taint := range masterNode.Spec.Taints {
			// If the taint exists, then remove it.
			// NOTE : We're assuming that the taint effect is 'NoSchedule'.
			if taint.Key == kubeadmConstants.LabelNodeRoleControlPlane {
				utils.ExecuteCommandOrDie(fmt.Sprintf(`
          kubectl taint node %s \
            node-role.kubernetes.io/control-plane:NoSchedule-
        `, masterNode.Name))
			}
		}
	}
}
