package core

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/git"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/kubernetes"
	argoCDV1Alpha1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	"github.com/go-git/go-git/v5/plumbing/transport"
)

func BootstrapCluster(ctx context.Context,
	skipKubePrometheusBuild,
	skipClusterctlMove bool,
	isPartOfDisasterRecovery bool,
) {
	// Detect git authentication method.
	gitAuthMethod := git.GetGitAuthMethod(ctx)

	// Create local dev environment.
	CreateDevEnv(ctx, skipKubePrometheusBuild)

	// While retrying, if `clusterctl move` has already been executed, then we skip the following
	// steps and jump to the disaster recovery setup step.
	provisionedClusterClient, err := kubernetes.CreateKubernetesClient(ctx, constants.OutputPathProvisionedClusterKubeconfig, false)
	if (err != nil) || !kubernetes.IsClusterctlMoveExecuted(ctx, provisionedClusterClient) {
		// Provision the main cluster
		provisionMainCluster(ctx, gitAuthMethod, skipKubePrometheusBuild)

		// Let the provisioned cluster manage itself.
		pivotCluster(ctx, gitAuthMethod, skipClusterctlMove, isPartOfDisasterRecovery)
	}

	// If the diasterRecovery section is specified in the cloud-provider specific config, then
	// setup Disaster Recovery.
	switch globals.CloudProviderName {
	case constants.CloudProviderAWS:
		if config.ParsedConfig.Cloud.AWS.DisasterRecovery != nil {
			globals.CloudProvider.SetupDisasterRecovery(ctx)
		}

	case constants.CloudProviderAzure:
		panic("unimplemented")

	case constants.CloudProviderHetzner:
		panic("unimplemented")

	default:
	}
}

func provisionMainCluster(ctx context.Context,
	gitAuthMethod transport.AuthMethod,
	skipKubePrometheusBuild bool,
) {
	managementClusterClient, _ := kubernetes.CreateKubernetesClient(ctx, constants.OutputPathManagementClusterKubeconfig, true)

	// Sync the complete capi-cluster ArgoCD App.
	//
	// BUG : If `clusterctl move` has already been executed, then we don't want to do this.
	kubernetes.SyncArgoCDApp(ctx, constants.ArgoCDAppCapiCluster, []*argoCDV1Alpha1.SyncOperationResource{})

	// Close ArgoCD application client.
	globals.ArgoCDApplicationClientCloser.Close()

	// Wait for the main cluster to be provisioned and ready.
	kubernetes.WaitForMainClusterToBeProvisioned(ctx, managementClusterClient)

	// Save kubeconfig locally.
	kubernetes.SaveKubeconfig(ctx, managementClusterClient)

	slog.Info("Cluster has been provisioned successfully 🎉🎉 !", slog.String("kubeconfig", constants.OutputPathProvisionedClusterKubeconfig))
}

func pivotCluster(ctx context.Context,
	gitAuthMethod transport.AuthMethod,
	skipClusterctlMove bool,
	isPartOfDisasterRecovery bool,
) {
	// Update the KUBECONFIG environment variable's value to the provisioned cluster's kubeconfig.
	os.Setenv("KUBECONFIG", constants.OutputPathProvisionedClusterKubeconfig)
	provisionedClusterClient, _ := kubernetes.CreateKubernetesClient(ctx, constants.OutputPathProvisionedClusterKubeconfig, true)

	// Wait for atleast 1 worker node to be initialized, so that we can deploy our application
	// workloads.
	kubernetes.WaitForMainClusterToBeReady(ctx, provisionedClusterClient)

	if isPartOfDisasterRecovery {
		// If this is a part of the disaster recovery process, then
		// restore Kubernetes Secrets containing a Sealed Secrets key.

		sealedSecretsBackupBucketName := globals.CloudProvider.GetSealedSecretsBackupBucketName()
		manifestsDirPath := utils.GetDownloadedStorageBucketContentsDir(sealedSecretsBackupBucketName)

		utils.ExecuteCommandOrDie(fmt.Sprintf("kubectl apply -f %s", manifestsDirPath))
	}

	// Install Sealed Secrets.
	kubernetes.InstallSealedSecrets(ctx)

	// We need to update the Sealed Secrets in the kubeaid-config fork.
	// Those represent Kubernetes Secrets encyrpted using the private key of the Sealed Secrets
	// controller installed in the K3d management cluster.
	// We need to update them, by encrypting the underlying Kubernetes Secrets using the private
	// key of the Sealed Secrets controller installed in the provisioned main cluster.
	SetupKubeAidConfig(ctx, gitAuthMethod, true)

	// Setup the provisioned cluster.
	SetupCluster(ctx, provisionedClusterClient)

	if !skipClusterctlMove {
		// In case of AWS, make ClusterAPI use IAM roles instead of (temporary) credentials.
		//
		// NOTE : The ClusterAPI AWS InfrastructureProvider component (CAPA controller) needs to run in
		//        a master node.
		//        And, the master node count should be more than 1.
		if config.ParsedConfig.Cloud.AWS != nil {
			// Zero the credentials CAPA controller started with.
			// This will force the CAPA controller to fall back to use the attached instance profiles.
			utils.ExecuteCommandOrDie("clusterawsadm controller zero-credentials --namespace capi-cluster")

			// Rollout and restart on capa-controller-manager deployment.
			utils.ExecuteCommandOrDie("clusterawsadm controller rollout-controller --namespace capi-cluster")
		}

		// Move ClusterAPI manifests to the provisioned cluster.
		utils.ExecuteCommandOrDie(fmt.Sprintf(
			"clusterctl move --kubeconfig %s --namespace %s --to-kubeconfig %s",
			constants.OutputPathManagementClusterKubeconfig, kubernetes.GetCapiClusterNamespace(), constants.OutputPathProvisionedClusterKubeconfig,
		))

		// Sync cluster-autoscaler ArgoCD App.
		kubernetes.SyncArgoCDApp(ctx, constants.ArgoCDAppClusterAutoscaler, []*argoCDV1Alpha1.SyncOperationResource{})
	}
}
