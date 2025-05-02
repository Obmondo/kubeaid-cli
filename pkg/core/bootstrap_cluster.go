package core

import (
	"context"
	"fmt"
	"log/slog"

	argoCDV1Alpha1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	"github.com/go-git/go-git/v5/plumbing/transport"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/git"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/kubernetes"
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
		)

		isClusterctlMoveExecuted := (err == nil) &&
			kubernetes.IsClusterctlMoveExecuted(ctx, provisionedClusterClient)

		// Provision and setup the main cluster.
		provisionAndSetupMainCluster(ctx, ProvisionAndSetupMainClusterArgs{
			CreateDevEnvArgs:         args.CreateDevEnvArgs,
			GitAuthMethod:            gitAuthMethod,
			IsClusterctlMoveExecuted: isClusterctlMoveExecuted,
		})

		if !args.SkipClusterctlMove && !isClusterctlMoveExecuted {
			// Pivot ClusterAPI (the provisioned cluster will manage itself).
			pivotCluster(ctx)
		}
	}

	// If the disasterRecovery section is specified in the cloud-provider specific config, then
	// setup Disaster Recovery.
	switch globals.CloudProviderName {
	case constants.CloudProviderAWS:
		if config.ParsedGeneralConfig.Cloud.AWS.DisasterRecovery != nil {
			globals.CloudProvider.SetupDisasterRecovery(ctx)
		}

	case constants.CloudProviderAzure:
		if config.ParsedGeneralConfig.Cloud.Azure.DisasterRecovery != nil {
			globals.CloudProvider.SetupDisasterRecovery(ctx)
		}
	}

	// Sync all ArgoCD Apps.
	kubernetes.SyncAllArgoCDApps(ctx)

	slog.InfoContext(ctx, "Cluster has been bootsrapped successfully 🎊")
}

type ProvisionAndSetupMainClusterArgs struct {
	*CreateDevEnvArgs
	GitAuthMethod            transport.AuthMethod
	IsClusterctlMoveExecuted bool
}

func provisionAndSetupMainCluster(ctx context.Context, args ProvisionAndSetupMainClusterArgs) {
	managementClusterClient := kubernetes.MustCreateKubernetesClient(ctx,
		kubernetes.GetManagementClusterKubeconfigPath(ctx),
	)

	if !args.IsClusterctlMoveExecuted {
		// Sync the complete capi-cluster ArgoCD App.
		kubernetes.SyncArgoCDApp(ctx, constants.ArgoCDAppCapiCluster,
			[]*argoCDV1Alpha1.SyncOperationResource{},
		)

		// Wait for the main cluster to be provisioned.
		kubernetes.WaitForMainClusterToBeProvisioned(ctx, managementClusterClient)

		// Save kubeconfig locally.
		kubernetes.SaveProvisionedClusterKubeconfig(ctx, managementClusterClient)

		slog.InfoContext(ctx,
			"Cluster has been provisioned successfully 🎉🎉 !",
			slog.String("kubeconfig", constants.OutputPathMainClusterKubeconfig),
		)
	}

	// Close ArgoCD application client (to the management cluster).
	_ = globals.ArgoCDApplicationClientCloser.Close()

	// Update the KUBECONFIG environment variable's value to the provisioned cluster's kubeconfig.
	// All Kubernetes operations from now on, will be done against the provisioned cluster.
	utils.MustSetEnv(constants.EnvNameKubeconfig, constants.OutputPathMainClusterKubeconfig)
	provisionedClusterClient := kubernetes.MustCreateKubernetesClient(ctx,
		constants.OutputPathMainClusterKubeconfig,
	)

	// Wait for atleast 1 worker node to be initialized, so that we can deploy our application
	// workloads.
	kubernetes.WaitForMainClusterToBeReady(ctx, provisionedClusterClient)

	/*
		Setup the provisioned cluster.

		NOTE : We need to update the Sealed Secrets in the kubeaid-config fork.
		       Currently, they represent Kubernetes Secrets encyrpted using the private key of the
		       Sealed Secrets controller installed in the K3d management cluster. We need to update
		       them, by encrypting the underlying Kubernetes Secrets using the private key of the
		       Sealed Secrets controller installed in the provisioned main cluster.
	*/
	SetupCluster(ctx, SetupClusterArgs{
		CreateDevEnvArgs:    args.CreateDevEnvArgs,
		IsManagementCluster: false,
		ClusterClient:       provisionedClusterClient,
		GitAuthMethod:       args.GitAuthMethod,
	})
}

func pivotCluster(ctx context.Context) {
	// In case of AWS, make ClusterAPI use IAM roles instead of (temporary) credentials.
	//
	// NOTE : The ClusterAPI AWS InfrastructureProvider component (CAPA controller) needs to run in
	//        a master node.
	if globals.CloudProviderName == constants.CloudProviderAWS {
		// Zero the credentials CAPA controller started with.
		// This will force the CAPA controller to fall back to use the attached instance profiles.
		utils.ExecuteCommandOrDie(
			"clusterawsadm controller zero-credentials --namespace capi-cluster",
		)

		// Rollout and restart on capa-controller-manager deployment.
		utils.ExecuteCommandOrDie(
			"clusterawsadm controller rollout-controller --namespace capi-cluster",
		)
	}

	// Move ClusterAPI manifests to the provisioned cluster.
	utils.ExecuteCommandOrDie(fmt.Sprintf(
		"clusterctl move --kubeconfig %s --namespace %s --to-kubeconfig %s",
		kubernetes.GetManagementClusterKubeconfigPath(ctx), kubernetes.GetCapiClusterNamespace(),
		constants.OutputPathMainClusterKubeconfig,
	))

	// Sync cluster-autoscaler ArgoCD App.
	kubernetes.SyncArgoCDApp(ctx, constants.ArgoCDAppClusterAutoscaler,
		[]*argoCDV1Alpha1.SyncOperationResource{},
	)
}
