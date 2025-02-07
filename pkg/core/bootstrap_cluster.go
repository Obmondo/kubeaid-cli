package core

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/Obmondo/kubeaid-bootstrap-script/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud/hetzner"
	"github.com/Obmondo/kubeaid-bootstrap-script/utils"
	argoCDV1Alpha1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	"github.com/go-git/go-git/v5/plumbing/transport"
)

func BootstrapCluster(ctx context.Context,
	skipKubePrometheusBuild,
	skipClusterctlMove bool,
	cloudProvider cloud.CloudProvider,
	isPartOfDisasterRecovery bool,
) {
	// Detect git authentication method.
	gitAuthMethod := utils.GetGitAuthMethod(ctx)

	// Create local dev environment.
	CreateDevEnv(ctx, skipKubePrometheusBuild)

	// While retrying, if `clusterctl move` has already been executed, then we skip the following
	// steps and jump to the disaster recovery setup step.
	provisionedClusterClient, err := utils.CreateKubernetesClient(ctx, constants.OutputPathProvisionedClusterKubeconfig, false)
	if (err != nil) || !utils.IsClusterctlMoveExecuted(ctx, provisionedClusterClient) {
		// Provision the main cluster
		provisionMainCluster(ctx, gitAuthMethod, skipKubePrometheusBuild)

		// Let the provisioned cluster manage itself.
		dogfoodProvisionedCluster(ctx, gitAuthMethod, skipClusterctlMove, cloudProvider, isPartOfDisasterRecovery)
	}

	// If the diasterRecovery section is specified in the cloud-provider specific config, then
	// setup Disaster Recovery.
	if config.ParsedConfig.Cloud.AWS.DisasterRecovery != nil {
		cloudProvider.SetupDisasterRecovery(ctx)
	}
}

func provisionMainCluster(ctx context.Context, gitAuthMethod transport.AuthMethod, skipKubePrometheusBuild bool) {
	managementClusterClient, _ := utils.CreateKubernetesClient(ctx, constants.OutputPathManagementClusterKubeconfig, true)

	// Sync the complete capi-cluster ArgoCD App.
	//
	// BUG : If `clusterctl move` has already been executed, then we don't want to do this.
	utils.SyncArgoCDApp(ctx, constants.ArgoCDAppCapiCluster, []*argoCDV1Alpha1.SyncOperationResource{})

	// Close ArgoCD application client.
	constants.ArgoCDApplicationClientCloser.Close()

	// CASE : Hetzner
	// Make the Failover IP point to the master node where `kubeadm init` has been executed.
	if config.ParsedConfig.Cloud.Hetzner != nil {
		hetzner.ExecuteFailoverScript(ctx)
	}

	// Wait for the main cluster to be provisioned and ready.
	utils.WaitForMainClusterToBeProvisioned(ctx, managementClusterClient)

	// Save kubeconfig locally.
	utils.SaveKubeconfig(ctx, managementClusterClient)

	slog.Info("Cluster has been provisioned successfully 🎉🎉 !", slog.String("kubeconfig", constants.OutputPathProvisionedClusterKubeconfig))
}

func dogfoodProvisionedCluster(ctx context.Context,
	gitAuthMethod transport.AuthMethod,
	skipClusterctlMove bool,
	cloudProvider cloud.CloudProvider,
	isPartOfDisasterRecovery bool,
) {
	// Update the KUBECONFIG environment variable's value to the provisioned cluster's kubeconfig.
	os.Setenv("KUBECONFIG", constants.OutputPathProvisionedClusterKubeconfig)
	provisionedClusterClient, _ := utils.CreateKubernetesClient(ctx, constants.OutputPathProvisionedClusterKubeconfig, true)

	// Wait for atleast 1 worker node to be initialized, so that we can deploy our application
	// workloads.
	utils.WaitForMainClusterToBeReady(ctx, provisionedClusterClient)

	if isPartOfDisasterRecovery {
		// If this is a part of the disaster recovery process, then
		// restore Kubernetes Secrets containing a Sealed Secrets key.

		sealedSecretsBackupBucketName := cloudProvider.GetSealedSecretsBackupBucketName()
		manifestsDirPath := utils.GetDownloadedStorageBucketContentsDir(sealedSecretsBackupBucketName)

		utils.ExecuteCommandOrDie(fmt.Sprintf("kubectl apply -f %s", manifestsDirPath))
	}

	// Install Sealed Secrets.
	utils.InstallSealedSecrets(ctx)

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
			constants.OutputPathManagementClusterKubeconfig, utils.GetCapiClusterNamespace(), constants.OutputPathProvisionedClusterKubeconfig,
		))

		// Sync cluster-autoscaler ArgoCD App.
		utils.SyncArgoCDApp(ctx, constants.ArgoCDAppClusterAutoscaler, []*argoCDV1Alpha1.SyncOperationResource{})
	}
}
