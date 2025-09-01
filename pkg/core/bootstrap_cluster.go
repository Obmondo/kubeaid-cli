// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package core

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path"

	argoCDV1Alpha1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/cluster-api-provider-aws/v2/cmd/clusterawsadm/controller/credentials"
	"sigs.k8s.io/cluster-api-provider-aws/v2/cmd/clusterawsadm/controller/rollout"
	"sigs.k8s.io/cluster-api/cmd/clusterctl/client"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud/hetzner"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
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

	// Create 'dev environment'.
	CreateDevEnv(ctx, args.CreateDevEnvArgs)

	// Provision and setup the main cluster.
	// The KUBECONFIG environment variable is also set to the main cluster's kubeconfig.
	provisionAndSetupMainCluster(ctx, ProvisionAndSetupMainClusterArgs{
		BootstrapClusterArgs: &args,
		GitAuthMethod:        gitAuthMethod,
	})

	// Construct main cluster client.
	mainClusterClient := kubernetes.MustCreateClusterClient(ctx,
		utils.MustGetEnv(constants.EnvNameKubeconfig),
	)

	// Setup Disaster Recovery, if the user wants.
	if config.ParsedGeneralConfig.Cloud.DisasterRecovery != nil {
		globals.CloudProvider.SetupDisasterRecovery(ctx)
	}

	// When this is part of a disaster recovery, we don't want to progress any further here,
	// but instead, restore the latest backup.
	if args.IsPartOfDisasterRecovery {
		return
	}

	// Sync all ArgoCD Apps.
	kubernetes.SyncAllArgoCDApps(ctx)

	// When we have setup Disaster Recovery,
	// trigger the first Velero and SealedSecret backups.
	if config.ParsedGeneralConfig.Cloud.DisasterRecovery != nil {
		// Create the first Velero backup.
		kubernetes.CreateBackup(ctx, "init", mainClusterClient)

		// Create first Sealed Secrets backup.
		kubernetes.TriggerCRONJob(ctx,
			types.NamespacedName{
				Name:      constants.CRONJobNameBackupSealedSecrets,
				Namespace: constants.NamespaceSealedSecrets,
			},
			mainClusterClient,
		)
	}

	slog.InfoContext(ctx, "Main cluster has been bootsrapped successfully 🎊")
}

type ProvisionAndSetupMainClusterArgs struct {
	*BootstrapClusterArgs
	GitAuthMethod transport.AuthMethod
}

func provisionAndSetupMainCluster(ctx context.Context, args ProvisionAndSetupMainClusterArgs) {
	switch globals.CloudProviderName {
	case constants.CloudProviderLocal:
		// When 'cloud provider = local', the K3d management cluster is the main cluster.
		// So, we don't need to do anything.
		return

	case constants.CloudProviderBareMetal:
		// When 'cloud provider = bare-metal', we're given a set of Linux servers whose lifecycle won't
		// be managed by us.
		// Since Machine lifecycle management is one of the core elements of the concept behind
		// ClusterAPI, ClusterAPI doesn't serve well in this case.
		// We'll be using Kubermatic KubeOne, to initialize the main cluster out of those Linux servers.
		provisionMainClusterUsingKubeOne(ctx)

	default:
		// Use ClusterAPI to provision the main cluster in the cloud.
		provisionMainClusterUsingClusterAPI(ctx)
	}

	// Close ArgoCD application client (to the management cluster).
	_ = globals.ArgoCDApplicationClientCloser.Close()

	// Update the KUBECONFIG environment variable's value to the provisioned cluster's kubeconfig.
	utils.MustSetEnv(constants.EnvNameKubeconfig, constants.OutputPathMainClusterKubeconfig)
	provisionedClusterClient := kubernetes.MustCreateClusterClient(ctx,
		constants.OutputPathMainClusterKubeconfig,
	)

	// Ensure that application workloads can be scheduled.
	if kubernetes.IsNodeGroupCountZero(ctx) {
		// If there are 0 node-groups, then we need to remove the NoSchedule taint from the master
		// nodes.
		kubernetes.RemoveNoScheduleTaintsFromMasterNodes(ctx, provisionedClusterClient)
	} else {
		// Otherwise, wait for atleast 1 worker node to be initialized.
		kubernetes.WaitForMainClusterToBeReady(ctx, provisionedClusterClient)
	}

	/*
		Setup the main cluster.

		NOTE : We need to update the Sealed Secrets in the kubeaid-config fork.
		       Currently, they represent Kubernetes Secrets encrypted using the private key of the
		       Sealed Secrets controller installed in the K3d management cluster. We need to update
		       them, by encrypting the underlying Kubernetes Secrets using the private key of the
		       Sealed Secrets controller installed in the provisioned main cluster.
	*/
	SetupCluster(ctx, SetupClusterArgs{
		CreateDevEnvArgs: args.CreateDevEnvArgs,
		ClusterType:      constants.ClusterTypeMain,
		ClusterClient:    provisionedClusterClient,
		GitAuthMethod:    args.GitAuthMethod,
	})

	if !kubernetes.UsingClusterAPI() {
		return
	}

	// Hold on!
	// When using ClusterAPI, we need to do a bit more for the main cluster setup.

	// Pivot ClusterAPI (the provisioned cluster will manage itself),
	// if enabled by the user and not alredy done.
	if !args.SkipClusterctlMove && !kubernetes.IsClusterctlMoveExecuted(ctx) {
		pivotCluster(ctx)
	}

	//nolint:staticcheck
	// Sync cluster-autoscaler ArgoCD App,
	// if not using Hetzner in bare-metal mode.
	if !((globals.CloudProviderName == constants.CloudProviderHetzner) &&
		(config.ParsedGeneralConfig.Cloud.Hetzner.Mode == constants.HetznerModeBareMetal)) {

		kubernetes.SyncArgoCDApp(ctx, constants.ArgoCDAppClusterAutoscaler,
			[]*argoCDV1Alpha1.SyncOperationResource{},
		)
	}

	// Sync the external-snapshotter ArgoCD App,
	// if not using Hetzner (since currently we don't support setting up disaster recovery for
	// Hetzner 🥴).
	if globals.CloudProviderName != constants.CloudProviderHetzner {
		kubernetes.SyncArgoCDApp(ctx, constants.ArgoCDExternalSnapshotter,
			[]*argoCDV1Alpha1.SyncOperationResource{},
		)
	}
}

func provisionMainClusterUsingClusterAPI(ctx context.Context) {
	// Determine whether 'clusterctl move' has been executed or not.
	// If yes, then we don't need to do anything.
	isClusterctlMoveExecuted := kubernetes.IsClusterctlMoveExecuted(ctx)
	if isClusterctlMoveExecuted {
		return
	}

	managementClusterClient := kubernetes.MustCreateClusterClient(ctx,
		kubernetes.GetManagementClusterKubeconfigPath(ctx),
	)

	// Sync the complete capi-cluster ArgoCD App.
	kubernetes.SyncArgoCDApp(ctx, constants.ArgoCDAppCapiCluster,
		[]*argoCDV1Alpha1.SyncOperationResource{},
	)

	// If provisioning cluster in Hetzner bare-metal, and using a Failover IP,
	// then we need to make the Failover IP point to the 'init master node'.
	// 'init master node' is the very first master node, where 'kubeadm init' is executed.
	if (globals.CloudProviderName == constants.CloudProviderHetzner) &&
		config.ControlPlaneInHetznerBareMetal() &&
		config.ParsedGeneralConfig.Cloud.Hetzner.ControlPlane.BareMetal.Endpoint.IsFailoverIP {

		hetznerCloudProvider, ok := globals.CloudProvider.(*hetzner.Hetzner)
		assert.Assert(ctx, ok, "Failed casting CloudProvider to Hetzner cloud-provider")

		hetznerCloudProvider.PointFailoverIPToInitMasterNode(ctx)
	}

	// Wait for the main cluster to be provisioned.
	kubernetes.WaitForMainClusterToBeProvisioned(ctx, managementClusterClient)

	// Save kubeconfig locally.
	kubernetes.SaveProvisionedClusterKubeconfig(ctx, managementClusterClient)

	slog.InfoContext(ctx,
		"Main cluster has been provisioned successfully 🎉🎉 !",
		slog.String("kubeconfig", constants.OutputPathMainClusterKubeconfig),
	)
}

func pivotCluster(ctx context.Context) {
	capiClusterNamespace := kubernetes.GetCapiClusterNamespace()

	// In case of AWS, make ClusterAPI use IAM roles instead of (temporary) credentials.
	//
	// NOTE : The ClusterAPI AWS InfrastructureProvider component (CAPA controller) needs to run in
	//        a master node.
	if globals.CloudProviderName == constants.CloudProviderAWS {
		// Zero the credentials CAPA controller started with.
		// This will force the CAPA controller to fall back to use the attached instance profiles.
		err := credentials.ZeroCredentials(credentials.ZeroCredentialsInput{
			Namespace: capiClusterNamespace,
		})
		assert.AssertErrNil(ctx, err, "Failed zeroing the credentials CAPA controller started with")

		// Rollout CAPA controller.
		err = rollout.RolloutControllers(rollout.RolloutControllersInput{
			Namespace: capiClusterNamespace,
		})
		assert.AssertErrNil(ctx, err, "Failed rolling out CAPA controller")
	}

	// Pause the ClusterAPI Infrastructure Provider in the management cluster,
	// and move the ClusterAPI manifests to the main cluster. They will be processed by the main
	// cluster's Infrastructure Provider.

	clusterctlClient, err := client.New(ctx, "")
	assert.AssertErrNil(ctx, err, "Failed constructing clusterctl client")

	err = clusterctlClient.Move(ctx, client.MoveOptions{
		FromKubeconfig: client.Kubeconfig{
			Path: kubernetes.GetManagementClusterKubeconfigPath(ctx),
		},

		ToKubeconfig: client.Kubeconfig{
			Path: constants.OutputPathMainClusterKubeconfig,
		},

		Namespace: capiClusterNamespace,
	})
	assert.AssertErrNil(ctx, err, "Failed pivoting the cluster by executing 'clusterctl move'")
}

func provisionMainClusterUsingKubeOne(ctx context.Context) {
	mainClusterName := config.ParsedGeneralConfig.Cluster.Name

	kubeoneDir := path.Join(utils.GetClusterDir(), "kubeone")

	slog.InfoContext(ctx, "Provisioning main cluster using Kubermatic KubeOne")

	utils.ExecuteCommandOrDie(fmt.Sprintf(
		"kubeone apply --manifest %s/kubeone-cluster.yaml --auto-approve",
		kubeoneDir,
	))

	// KubeOne backups the main cluster's PKI infrastructure in a .tar.gz file locally.
	// We don't need it.
	err := os.Remove(fmt.Sprintf("%s/%s.tar.gz", kubeoneDir, mainClusterName))
	assert.AssertErrNil(ctx, err,
		"Failed deleting main cluster's PKI infrastructure backup",
	)

	/*
		KubeOne also saves the main cluster's kubeconfig locally.
		Let's move that kubeconfig file to our standardized location for the main cluster's kubeconfig
		file.

		NOTE : When KubeAid Bootstrap Script runs inside a container, with the outputs folder mounted
		       from the host, using os.Rename( ) to do the move operation fails with error :

		         rename kubeaid-demo-bare-metal-kubeconfig outputs/kubeconfigs/clusters/main.yaml: invalid cross-device link

		       since those files exist on separate drives.
	*/
	kubeoneGeneratedKubeconfigFilePath := fmt.Sprintf("%s-kubeconfig", mainClusterName)
	utils.MustMoveFile(ctx,
		kubeoneGeneratedKubeconfigFilePath, constants.OutputPathMainClusterKubeconfig,
	)

	slog.InfoContext(ctx,
		"Main cluster has been provisioned successfully 🎉🎉 !",
		slog.String("kubeconfig", constants.OutputPathMainClusterKubeconfig),
	)
}
