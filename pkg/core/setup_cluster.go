package core

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/kubernetes"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
	argoCDV1Alpha1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	"github.com/go-git/go-git/v5/plumbing/transport"
	coreV1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type SetupClusterArgs struct {
	*CreateDevEnvArgs

	IsManagementCluster bool
	ClusterClient       client.Client

	GitAuthMethod transport.AuthMethod
}

func SetupCluster(ctx context.Context, args SetupClusterArgs) {
	clusterType := constants.ClusterTypeManagement
	if !args.IsManagementCluster {
		clusterType = constants.ClusterTypeMain
	}

	slog.InfoContext(ctx, "Setting up cluster....", slog.String("cluster-type", clusterType))

	// Install Sealed Secrets.
	kubernetes.InstallSealedSecrets(ctx)

	// If we're recovering a cluster, then we need to restore the Sealed Secrets controller private
	// keys from a previous cluster which got destroyed.
	if args.IsPartOfDisasterRecovery {
		sealedSecretsKeysBackupBucketName := config.ParsedConfig.Cloud.AWS.DisasterRecovery.SealedSecretsBackupS3BucketName
		sealedSecretsKeysDirPath := utils.GetDownloadedStorageBucketContentsDir(sealedSecretsKeysBackupBucketName)

		utils.ExecuteCommandOrDie(fmt.Sprintf("kubectl apply -f %s", sealedSecretsKeysDirPath))

		slog.InfoContext(ctx,
			"Restored Sealed Secrets controller private keys from a previous cluster",
			slog.String("dir-path", sealedSecretsKeysDirPath),
		)
	}

	// Setup cluster directory in the user's KubeAid config repo.
	SetupKubeAidConfig(ctx, SetupKubeAidConfigArgs{
		CreateDevEnvArgs: args.CreateDevEnvArgs,
		GitAuthMethod:    args.GitAuthMethod,
	})

	// Install and setup ArgoCD.
	kubernetes.InstallAndSetupArgoCD(ctx, utils.GetClusterDir(), args.ClusterClient)

	// Create the capi-cluster / capi-cluster-<customer-id> namespace, where the 'cloud-credentials'
	// Kubernetes Secret will exist.
	kubernetes.CreateNamespace(ctx, kubernetes.GetCapiClusterNamespace(), args.ClusterClient)

	// Sync the Root, CertManager and Secrets ArgoCD Apps one by one.
	argocdAppsToBeSynced := []string{
		"root",
		"cert-manager",
		"secrets",
	}

	for _, argoCDApp := range argocdAppsToBeSynced {
		kubernetes.SyncArgoCDApp(ctx, argoCDApp, []*argoCDV1Alpha1.SyncOperationResource{})
	}

	if config.ParsedConfig.Cloud.Local != nil {
		return
	}

	// If trying to provision a main cluster in some cloud provider like AWS / Azure / Hetzner.
	// Sync ClusterAPI ArgoCD App.
	kubernetes.SyncArgoCDApp(ctx, "cluster-api", []*argoCDV1Alpha1.SyncOperationResource{})
	//
	// Sync the Infrastructure Provider component of the capi-cluster ArgoCD App.
	// TODO : Use ArgoCD sync waves so that we don't need to explicitly sync the Infrastructure
	//        Provider component first.
	syncInfrastructureProvider(ctx, args.ClusterClient)

	printHelpTextForArgoCDDashboardAccess(clusterType)
}

// Syncs the Infrastructure Provider component of the CAPI Cluster ArgoCD App and waits for the
// infrastructure specific CRDs to be installed and pod to be running.
func syncInfrastructureProvider(ctx context.Context, clusterClient client.Client) {
	// Sync the Infrastructure Provider component.
	kubernetes.SyncArgoCDApp(ctx, constants.ArgoCDAppCapiCluster, []*argoCDV1Alpha1.SyncOperationResource{
		{
			Group: "operator.cluster.x-k8s.io",
			Kind:  "InfrastructureProvider",
			Name:  getInfrastructureProviderName(),
		},
	})

	capiClusterNamespace := kubernetes.GetCapiClusterNamespace()

	// Wait for the infrastructure specific CRDs to be installed and infrastructure provider component
	// pod to be running.

	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("namespace", capiClusterNamespace),
	})

	wait.PollUntilContextCancel(ctx, time.Minute, false, func(ctx context.Context) (bool, error) {
		podList := &coreV1.PodList{}
		err := clusterClient.List(ctx, podList, &client.ListOptions{
			Namespace: capiClusterNamespace,
		})
		assert.AssertErrNil(ctx, err, "Failed listing pods")

		if (len(podList.Items) > 0) && (podList.Items[0].Status.Phase == coreV1.PodRunning) {
			return true, nil
		}

		slog.InfoContext(ctx, "Waiting for the infrastructure provider component pod to come up")
		return false, nil
	})
}

// Returns the name of the InfrastructureProvider component.
func getInfrastructureProviderName() string {
	infrastructureProviderName := globals.CloudProviderName

	if len(config.ParsedConfig.CustomerID) > 0 {
		infrastructureProviderName = infrastructureProviderName + "-" + config.ParsedConfig.CustomerID
	}

	return infrastructureProviderName
}

func printHelpTextForArgoCDDashboardAccess(clusterType string) {
	clusterKubeconfigPath := constants.OutputPathManagementClusterHostKubeconfig
	if clusterType == constants.ClusterTypeMain {
		clusterKubeconfigPath = constants.OutputPathMainClusterKubeconfig
	}

	// Print out help text for the user to access ArgoCD admin dashboard.
	helpText := fmt.Sprintf(
		`
Finished setting up %s cluster.

To access the ArgoCD admin dashboard :

  (1) In your host machine's terminal, navigate to the directory from where you executed the
      script (you'll notice the outputs/ directory there). Do :

        export KUBECONFIG=%s

  (2) Retrieve the ArgoCD admin password :

        kubectl get secret argo-cd-argocd-initial-admin-secret --namespace argo-cd \
          -o jsonpath="{.data.password}" | base64 -d

  (3) Port forward ArgoCD server :

  kubectl port-forward svc/argo-cd-argocd-server --namespace argo-cd 8080:443

  (4) Visit https://localhost:8080 in a browser and login to ArgoCD as admin.
    `,
		clusterType,
		clusterKubeconfigPath,
	)
	println(helpText)
}
