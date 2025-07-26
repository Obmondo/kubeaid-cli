package core

import (
	"context"
	"fmt"
	"log/slog"
	"path"
	"time"

	argoCDV1Aplha1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	yqCmdLib "github.com/mikefarah/yq/v4/cmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/git"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/kubernetes"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
)

type (
	UpgradeClusterArgs struct {
		NewKubernetesVersion string
		CloudSpecificUpdates any

		SkipPRWorkflow bool
	}
)

func UpgradeCluster(ctx context.Context, args UpgradeClusterArgs) {
	// Update the values-capi-cluster.yaml file in the kubeaid-config repo.
	updateCapiClusterValuesFile(ctx, &args)

	// Set KUBECONFIG environment variable.
	utils.MustSetEnv(constants.EnvNameKubeconfig, constants.OutputPathMainClusterKubeconfig)
	//
	// If 'clusterctl move' wasn't executed, then we need to communicate with the management
	// cluster instead.
	if !kubernetes.IsClusterctlMoveExecuted(ctx) {
		utils.MustSetEnv(
			constants.EnvNameKubeconfig, kubernetes.GetManagementClusterKubeconfigPath(ctx),
		)
	}

	// Construct the Kubernetes cluster client.
	clusterClient := kubernetes.MustCreateClusterClient(ctx,
		utils.MustGetEnv(constants.EnvNameKubeconfig),
	)

	{
		// Port-forward ArgoCD and create ArgoCD client.
		argoCDClient := kubernetes.NewArgoCDClient(ctx, clusterClient)

		// Create ArgoCD application client.
		globals.ArgoCDApplicationClientCloser, globals.ArgoCDApplicationClient = argoCDClient.NewApplicationClientOrDie()
		defer globals.ArgoCDApplicationClientCloser.Close()
	}

	// (1) Upgrading the Control Plane.
	upgradeControlPlane(ctx, args, clusterClient)

	// (2) Upgrading each node-group one by one.
	switch globals.CloudProviderName {
	case constants.CloudProviderAWS:
		for _, nodeGroup := range config.ParsedGeneralConfig.Cloud.AWS.NodeGroups {
			upgradeNodeGroup(ctx, args, clusterClient, nodeGroup.Name)
		}

	case constants.CloudProviderAzure:
		for _, nodeGroup := range config.ParsedGeneralConfig.Cloud.Azure.NodeGroups {
			upgradeNodeGroup(ctx, args, clusterClient, nodeGroup.Name)
		}

	default:
		panic("unimplemented")
	}
}

// Update the values-capi-cluster.yaml file in the kubeaid-config repo.
// Once the kubeaid-config changes get merged to the default merged,
// only then we'll trigger the actual rollout process.
func updateCapiClusterValuesFile(ctx context.Context, args *UpgradeClusterArgs) {
	// Detect git authentication method.
	gitAuthMethod := git.GetGitAuthMethod(ctx)

	// Clone the KubeAid config fork locally (if not already cloned).
	repo := git.CloneRepo(ctx,
		config.ParsedGeneralConfig.Forks.KubeaidConfigForkURL,
		utils.GetKubeAidConfigDir(),
		gitAuthMethod,
	)

	workTree, err := repo.Worktree()
	assert.AssertErrNil(ctx, err, "Failed getting kubeaid-config repo worktree")

	defaultBranchName := git.GetDefaultBranchName(ctx, gitAuthMethod, repo)

	/*
		Decide the branch, where we want to do the changes :

		  (1) If the user has provided the --skip-pr-flow flag, then we'll do the changes in and push
		      them directly to the default branch.

		  (2) Otherwise, we'll create and checkout to a new branch. Changes will be done in and pushed
		      to that new branch. The user then needs to manually review the changes, create a PR and
		      merge it to the default branch.
	*/
	targetBranchName := defaultBranchName
	if !args.SkipPRWorkflow {
		// Create and checkout to a new branch.
		newBranchName := fmt.Sprintf("kubeaid-%s-%d",
			config.ParsedGeneralConfig.Cluster.Name,
			time.Now().Unix(),
		)
		git.CreateAndCheckoutToBranch(ctx, repo, newBranchName, workTree, gitAuthMethod)

		targetBranchName = newBranchName
	}

	// Update values-capi-cluster.yaml file (using yq).
	{
		capiClusterValuesFilePath := path.Join(
			utils.GetClusterDir(),
			"argocd-apps/values-capi-cluster.yaml",
		)

		// If the user wants Kubernetes version upgrade,
		// then update the Kubernetes version.
		if len(args.NewKubernetesVersion) > 0 {
			yqCmd := yqCmdLib.New()
			yqCmd.SetArgs([]string{
				"eval",
				fmt.Sprintf("(.global.kubernetes.version) = \"%s\"", args.NewKubernetesVersion),
				capiClusterValuesFilePath,
				"--inplace",
			})
			err := yqCmd.ExecuteContext(ctx)
			assert.AssertErrNil(ctx, err,
				"Failed updating Kubernetes version in values-capi-cluster.yaml file",
			)
		}

		// If the user wants OS upgrade,
		// then make necessary cloud provider specific updates.
		globals.CloudProvider.UpdateCapiClusterValuesFileWithCloudSpecificDetails(ctx,
			capiClusterValuesFilePath,
			args.CloudSpecificUpdates,
		)
	}

	// Add, commit and push the changes.
	commitMessage := fmt.Sprintf("(cluster/%s) : updated values-capi-cluster.yaml",
		config.ParsedGeneralConfig.Cluster.Name,
	)
	commitHash := git.AddCommitAndPushChanges(ctx,
		repo,
		workTree,
		targetBranchName,
		gitAuthMethod,
		config.ParsedGeneralConfig.Cluster.Name,
		commitMessage,
	)

	if !args.SkipPRWorkflow {
		/*
			The user now needs to go ahead and create a PR from the new to the default branch. Then he
			needs to merge that branch.

			NOTE : We can't create the PR for the user, since PRs are not part of the core git lib.
						 They are specific to the git platform the user is on.
		*/

		// Wait until the user creates a PR and merges it to the default branch.
		git.WaitUntilPRMerged(ctx, repo, defaultBranchName, commitHash, gitAuthMethod, targetBranchName)
	}
}

func upgradeControlPlane(
	ctx context.Context,
	args UpgradeClusterArgs,
	clusterClient client.Client,
) {
	slog.InfoContext(ctx, "Triggering control plane upgrade")

	// If the user wants to do an OS upgrade, then make necessary cloud provider specific updates in
	// the corresponding machine template, by deleting and recreating it.
	// Since machine templates are immutable, we cannot update them directly.
	//
	// REFER : https://cluster-api.sigs.k8s.io/tasks/upgrading-clusters#upgrading-the-control-plane-machines.
	globals.CloudProvider.UpdateMachineTemplate(ctx, clusterClient, args.CloudSpecificUpdates)

	// If the user wants a Kubernetes version upgrade, then
	// update the Kubernetes version in the KubeadmControlPlane resource.
	// We'll do this, by syncing it specifically, in the capi-cluster ArgoCD App.
	if len(args.NewKubernetesVersion) > 0 {
		kubeadmControlPlaneName := fmt.Sprintf("%s-control-plane",
			config.ParsedGeneralConfig.Cluster.Name,
		)

		// Sync capi-cluster ArgoCD App's KubeadmControlPlane resource.
		kubernetes.SyncArgoCDApp(ctx, constants.ArgoCDAppCapiCluster,
			[]*argoCDV1Aplha1.SyncOperationResource{
				{
					Group: "controlplane.cluster.x-k8s.io",
					Kind:  "KubeadmControlPlane",
					Name:  kubeadmControlPlaneName,
				},
			},
		)

		// Rollout the control-plane, immediately
		utils.ExecuteCommandOrDie(fmt.Sprintf(
			"clusterctl alpha rollout restart kubeadmcontrolplane/%s -n %s",
			kubeadmControlPlaneName, kubernetes.GetCapiClusterNamespace(),
		))
	}
}

func upgradeNodeGroup(ctx context.Context,
	args UpgradeClusterArgs,
	clusterClient client.Client,
	nodeGroupName string,
) {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("node-group", nodeGroupName),
	})

	slog.InfoContext(ctx, "Triggering node-group upgrade")

	// If the user wants to do an OS upgrade, then make necessary cloud provider specific updates in
	// the corresponding machine template, by deleting and recreating it.
	// Since machine templates are immutable, we cannot update them directly.
	//
	// REFER : https://cluster-api.sigs.k8s.io/tasks/upgrading-clusters#upgrading-the-control-plane-machines.
	globals.CloudProvider.UpdateMachineTemplate(ctx, clusterClient, args.CloudSpecificUpdates)

	/*
		If the user wants a Kubernetes version upgrade,
		then update the Kubernetes version in the KubeadmConfigTemplate and MachineDeployment
		resources corresponding to the node-group.
		We'll do this, by syncing them specifically, in the capi-cluster ArgoCD App.

		NOTE : When calculating diff, we ignore the .spec.replicas field for the MachineDeployment
		       resource. So, syncing it shouldn't create any difference in the node-group's current
		       replica count.
	*/
	if len(args.NewKubernetesVersion) > 0 {
		kubeadmConfigTemplateName := fmt.Sprintf("%s-%s",
			config.ParsedGeneralConfig.Cluster.Name,
			nodeGroupName,
		)

		machineDeploymentName := fmt.Sprintf("%s-%s",
			config.ParsedGeneralConfig.Cluster.Name,
			nodeGroupName,
		)

		// Sync capi-cluster ArgoCD App's KubeadmConfigTemplate and MachineDeployment resources
		// corresponding to the node-group.
		kubernetes.SyncArgoCDApp(ctx, constants.ArgoCDAppCapiCluster,
			[]*argoCDV1Aplha1.SyncOperationResource{
				{
					Group: "bootstrap.cluster.x-k8s.io",
					Kind:  "KubeadmConfigTemplate",
					Name:  kubeadmConfigTemplateName,
				},
				{
					Group: "cluster.x-k8s.io",
					Kind:  "MachineDeployment",
					Name:  machineDeploymentName,
				},
			},
		)

		// Rollout the node-group, immediately.
		utils.ExecuteCommandOrDie(fmt.Sprintf(
			"clusterctl alpha rollout restart machinedeployment/%s -n %s",
			machineDeploymentName, kubernetes.GetCapiClusterNamespace(),
		))
	}
}
