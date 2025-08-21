// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package core

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	argoCDV1Alpha1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	"github.com/go-git/go-git/v5/plumbing/transport"
	coreV1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	clusterctlV1 "sigs.k8s.io/cluster-api/cmd/clusterctl/api/v1alpha3"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud/azure"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	gitUtils "github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/git"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/kubernetes"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
)

type SetupClusterArgs struct {
	*CreateDevEnvArgs

	ClusterType   string
	ClusterClient client.Client

	GitAuthMethod transport.AuthMethod
}

func SetupCluster(ctx context.Context, args SetupClusterArgs) {
	slog.InfoContext(ctx, "Setting up cluster....", slog.String("cluster-type", args.ClusterType))

	{
		// Clone the KubeAid fork locally (if not already cloned).
		kubeAidRepo := gitUtils.CloneRepo(ctx,
			config.ParsedGeneralConfig.Forks.KubeaidForkURL,
			constants.KubeAidDirectory,
			args.GitAuthMethod,
		)

		// Hard reset to the KubeAid tag mentioned in the KubeAid Bootstrap Script general config file.
		gitUtils.HardResetRepoToTag(ctx,
			kubeAidRepo,
			config.ParsedGeneralConfig.Cluster.KubeaidVersion,
		)
	}

	// Create required namespaces before syncing all the ArgoCD Apps.
	// Otherwise, some syncing of ArgoCD Apps might fail.
	// For e.g. : syncing of the kube-prometheus ArgoCD App fails if the obmondo namespace doesn't
	// exist.
	namespacesToBeCreated := []string{
		"crossplane",
		"obmondo",
	}
	for _, namespace := range namespacesToBeCreated {
		kubernetes.CreateNamespace(ctx, namespace, args.ClusterClient)
	}

	// If recovering a cluster, then restore the Sealed Secrets controller private keys.
	if args.IsPartOfDisasterRecovery {
		// Create the sealed-secrets namespace.
		kubernetes.CreateNamespace(ctx, constants.NamespaceSealedSecrets, args.ClusterClient)

		sealedSecretsKeysBackupBucketName := config.ParsedGeneralConfig.Cloud.DisasterRecovery.SealedSecretsBackupsBucketName
		sealedSecretsKeysDirPath := utils.GetDownloadedStorageBucketContentsDir(
			sealedSecretsKeysBackupBucketName,
		)

		/*
			Restore the Sealed Secrets controller private keys.

			NOTE : The first time we do kubectl apply, resourceVersion of the SealedSecrets change.
			       Because of which, doing kubectl apply for the second time errors out, thus hindering
			       the script's idempotency.
		*/
		utils.ExecuteCommandOrDie(
			fmt.Sprintf("kubectl replace --force -f %s", sealedSecretsKeysDirPath),
		)

		slog.InfoContext(ctx,
			"Restored Sealed Secrets controller private keys",
			slog.String("dir-path", sealedSecretsKeysDirPath),
		)
	}

	// Install Sealed Secrets.
	kubernetes.InstallSealedSecrets(ctx)

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
	argoCDAppsToBeSynced := []string{
		constants.ArgoCDAppRoot,
		"cert-manager",
		"secrets",
	}
	for _, argoCDApp := range argoCDAppsToBeSynced {
		kubernetes.SyncArgoCDApp(ctx, argoCDApp, []*argoCDV1Alpha1.SyncOperationResource{})
	}

	// Any cloud provider specific tasks.
	switch globals.CloudProviderName {
	case constants.CloudProviderAzure:
		// Install CrossPlane.
		// Then set it up, by installing required Providers, Functions, Compositions and
		// Composite Resource Definitions (XRDs).
		kubernetes.InstallAndSetupCrossplane(ctx)

		// Doing the following once (i.e., while being in the management cluster) is enough.
		if args.ClusterType == constants.ClusterTypeManagement {
			cloudProviderAzure := azure.CloudProviderToAzure(ctx, globals.CloudProvider)

			// Create required infrastructure for Azure Workload Identity and Disaster Recovery,
			// using CrossPlane.
			cloudProviderAzure.ProvisionInfrastructure(ctx)

			// Create the OIDC provider.
			cloudProviderAzure.CreateOIDCProvider(ctx)

			// Retrieves details about the infrastructure provisioned using CrossPlane.
			cloudProviderAzure.GetInfrastructureDetails(ctx, args.ClusterClient)

			// Rebuild the cluster's KubeAid Config, with the infrastructure details available.
			SetupKubeAidConfig(ctx, SetupKubeAidConfigArgs{
				CreateDevEnvArgs: args.CreateDevEnvArgs,
				GitAuthMethod:    args.GitAuthMethod,
			})
		}
	}

	// When using ClusterAPI to provision the main cluster.
	if kubernetes.UsingClusterAPI() {
		// Sync ClusterAPI Operator ArgoCD App.
		kubernetes.SyncArgoCDApp(ctx, "cluster-api-operator",
			[]*argoCDV1Alpha1.SyncOperationResource{},
		)

		//nolint:godox
		// Sync the Infrastructure Provider component of the capi-cluster ArgoCD App.
		// TODO : Use ArgoCD sync waves so that we don't need to explicitly sync the Infrastructure
		//        Provider component first.
		syncInfrastructureProvider(ctx, args.ClusterClient)
	}

	printHelpTextForArgoCDDashboardAccess(args.ClusterType)
}

// Syncs the Infrastructure Provider component of the CAPI Cluster ArgoCD App and waits for the
// infrastructure specific CRDs to be installed and pod to be running.
func syncInfrastructureProvider(ctx context.Context, clusterClient client.Client) {
	// Sync the Infrastructure Provider component.
	kubernetes.SyncArgoCDApp(ctx, constants.ArgoCDAppCapiCluster,
		[]*argoCDV1Alpha1.SyncOperationResource{
			{
				Group: "operator.cluster.x-k8s.io",
				Kind:  string(clusterctlV1.InfrastructureProviderType),
				Name:  getInfrastructureProviderName(),
			},
		},
	)

	capiClusterNamespace := kubernetes.GetCapiClusterNamespace()

	// Wait for the infrastructure specific CRDs to be installed and infrastructure provider component
	// pod to be running.

	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("namespace", capiClusterNamespace),
	})

	err := wait.PollUntilContextCancel(ctx, time.Minute, false,
		func(ctx context.Context) (bool, error) {
			podList := &coreV1.PodList{}
			err := clusterClient.List(ctx, podList, &client.ListOptions{
				Namespace: capiClusterNamespace,
			})
			assert.AssertErrNil(ctx, err, "Failed listing pods")

			if (len(podList.Items) > 0) && (podList.Items[0].Status.Phase == coreV1.PodRunning) {
				return true, nil
			}

			slog.InfoContext(ctx,
				"Waiting for the infrastructure provider component pod to come up",
			)
			return false, nil
		},
	)
	assert.AssertErrNil(ctx, err,
		"Failed waiting for the infrastructure provider component to come up",
	)
}

// Returns the name of the InfrastructureProvider component.
func getInfrastructureProviderName() string {
	infrastructureProviderName := globals.CloudProviderName

	if config.ParsedGeneralConfig.Obmondo != nil {
		infrastructureProviderName = infrastructureProviderName + "-" + config.ParsedGeneralConfig.Obmondo.CustomerID
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

        echo "ArgoCD admin password : "
        kubectl get secret argocd-initial-admin-secret --namespace argocd \
          -o jsonpath="{.data.password}" | base64 -d

  (3) Port forward ArgoCD server :

        kubectl port-forward svc/argocd-server --namespace argocd 8080:443

  (4) Visit https://localhost:8080 in a browser and login to ArgoCD as admin.
    `,
		clusterType,
		clusterKubeconfigPath,
	)
	fmt.Println(helpText) //nolint: forbidigo, revive
}
