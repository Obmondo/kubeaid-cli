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

type BootstrapClusterArgs struct {
	*CreateDevEnvArgs
	SkipClusterctlMove bool
}

func BootstrapCluster(ctx context.Context, args BootstrapClusterArgs) {
	// Detect git authentication method.
	gitAuthMethod := git.GetGitAuthMethod(ctx)

	// Create local dev environment.
	CreateDevEnv(ctx, args.CreateDevEnvArgs)

	if globals.CloudProviderName != constants.CloudProviderLocal {
		provisionedClusterClient, err := kubernetes.CreateKubernetesClient(ctx,
			constants.OutputPathMainClusterKubeconfig,
			false,
		)
		isClusterctlMoveExecuted := (err == nil) && kubernetes.IsClusterctlMoveExecuted(ctx, provisionedClusterClient)
		if !isClusterctlMoveExecuted {
			// Provision and setup the main cluster.
			provisionAndSetupMainCluster(ctx, ProvisionAndSetupMainClusterArgs{
				CreateDevEnvArgs: args.CreateDevEnvArgs,
				GitAuthMethod:    gitAuthMethod,
			})

			if !args.SkipClusterctlMove {
				// Pivot ClusterAPI (the provisioned cluster will manage itself).
				pivotCluster(ctx, gitAuthMethod, args.SkipClusterctlMove, args.IsPartOfDisasterRecovery)
			}
		} else {
			// We're retrying running the script.

			// Update the KUBECONFIG environment variable's value to the provisioned cluster's kubeconfig.
			// All Kubernetes operations from now on, will be done against the provisioned cluster.
			os.Setenv(constants.EnvNameKubeconfig, constants.OutputPathMainClusterKubeconfig)

			// We need to use the ArgoCD Application client to the provisioned cluster's ArgoCD server.
			kubernetes.RecreateArgoCDApplicationClient(ctx, provisionedClusterClient)
		}
	}

	// If the disasterRecovery section is specified in the cloud-provider specific config, then
	// setup Disaster Recovery.
	switch globals.CloudProviderName {
	case constants.CloudProviderAWS:
		if config.ParsedGeneralConfig.Cloud.AWS.DisasterRecovery != nil {
			globals.CloudProvider.SetupDisasterRecovery(ctx)
		}
	}

	// Sync all ArgoCD Apps.
	kubernetes.SyncAllArgoCDApps(ctx)

	slog.InfoContext(ctx, "Cluster has been bootsrapped successfully 🎊")
}

type ProvisionAndSetupMainClusterArgs struct {
	*CreateDevEnvArgs
	GitAuthMethod transport.AuthMethod
}

func provisionAndSetupMainCluster(ctx context.Context, args ProvisionAndSetupMainClusterArgs) {
	managementClusterClient, _ := kubernetes.CreateKubernetesClient(ctx,
		kubernetes.GetManagementClusterKubeconfigPath(ctx),
		true,
	)

	// Sync the complete capi-cluster ArgoCD App.
	kubernetes.SyncArgoCDApp(ctx, constants.ArgoCDAppCapiCluster, []*argoCDV1Alpha1.SyncOperationResource{})

	// Close ArgoCD application client (to the management cluster).
	globals.ArgoCDApplicationClientCloser.Close()

	// Wait for the main cluster to be provisioned.
	kubernetes.WaitForMainClusterToBeProvisioned(ctx, managementClusterClient)

	// Save kubeconfig locally.
	kubernetes.SaveKubeconfig(ctx, managementClusterClient)

	slog.Info("Cluster has been provisioned successfully 🎉🎉 !", slog.String("kubeconfig", constants.OutputPathMainClusterKubeconfig))

	// Update the KUBECONFIG environment variable's value to the provisioned cluster's kubeconfig.
	// All Kubernetes operations from now on, will be done against the provisioned cluster.
	os.Setenv(constants.EnvNameKubeconfig, constants.OutputPathMainClusterKubeconfig)
	provisionedClusterClient, _ := kubernetes.CreateKubernetesClient(ctx, constants.OutputPathMainClusterKubeconfig, true)

	// Wait for atleast 1 worker node to be initialized, so that we can deploy our application
	// workloads.
	kubernetes.WaitForMainClusterToBeReady(ctx, provisionedClusterClient)

	// Setup the provisioned cluster.
	//
	// NOTE : We need to update the Sealed Secrets in the kubeaid-config fork.
	// Currently, they represent Kubernetes Secrets encyrpted using the private key of the Sealed
	// Secrets controller installed in the K3d management cluster.
	// We need to update them, by encrypting the underlying Kubernetes Secrets using the private
	// key of the Sealed Secrets controller installed in the provisioned main cluster.
	SetupCluster(ctx, SetupClusterArgs{
		CreateDevEnvArgs:    args.CreateDevEnvArgs,
		IsManagementCluster: false,
		ClusterClient:       provisionedClusterClient,
		GitAuthMethod:       args.GitAuthMethod,
	})
}

func pivotCluster(ctx context.Context,
	gitAuthMethod transport.AuthMethod,
	skipClusterctlMove bool,
	isPartOfDisasterRecovery bool,
) {
	// In case of AWS, make ClusterAPI use IAM roles instead of (temporary) credentials.
	//
	// NOTE : The ClusterAPI AWS InfrastructureProvider component (CAPA controller) needs to run in
	//        a master node.
	//        And, the master node count should be more than 1.
	if globals.CloudProviderName == constants.CloudProviderAWS {
		// Zero the credentials CAPA controller started with.
		// This will force the CAPA controller to fall back to use the attached instance profiles.
		utils.ExecuteCommandOrDie("clusterawsadm controller zero-credentials --namespace capi-cluster")

		// Rollout and restart on capa-controller-manager deployment.
		utils.ExecuteCommandOrDie("clusterawsadm controller rollout-controller --namespace capi-cluster")
	}

	// Move ClusterAPI manifests to the provisioned cluster.
	utils.ExecuteCommandOrDie(fmt.Sprintf(
		"clusterctl move --kubeconfig %s --namespace %s --to-kubeconfig %s",
		kubernetes.GetManagementClusterKubeconfigPath(ctx), kubernetes.GetCapiClusterNamespace(),
		constants.OutputPathMainClusterKubeconfig,
	))

	// Sync cluster-autoscaler ArgoCD App.
	kubernetes.SyncArgoCDApp(ctx, constants.ArgoCDAppClusterAutoscaler, []*argoCDV1Alpha1.SyncOperationResource{})
}
