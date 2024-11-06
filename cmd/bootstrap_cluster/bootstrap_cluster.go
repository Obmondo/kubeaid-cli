package bootstrap_cluster

import (
	"fmt"
	"log"
	"log/slog"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/Obmondo/kubeaid-bootstrap-script/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/utils"
	argoCDV1Alpha1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/urfave/cli/v2"
)

var currentTime = time.Now().Unix()

func BootstrapCluster(ctx *cli.Context) error {
	// Initialize temp dir where repositories will be cloned.
	utils.InitTempDir()

	// Parse the config file.
	configFilePath := ctx.Path(constants.FlagNameConfigFile)
	constants.ParsedConfig = config.ParseConfigFile(configFilePath)

	// Set environment variables.
	utils.SetEnvs()

	// Detect git authentication method.
	gitAuthMethod := utils.GetGitAuthMethod()

	// Create the management cluster (using K3d), if it doesn't alredy exist.
	managementClusterName := "management-cluster"
	if !utils.DoesK3dClusterExist(managementClusterName) {
		slog.Info("Spinning up K3d management cluster (in the host machine)", slog.String("name", managementClusterName))
		utils.ExecuteCommandOrDie(fmt.Sprintf(`
			k3d cluster create %s \
				--servers 1 --agents 2 \
				--image rancher/k3s:v1.31.1-k3s1 \
				--wait
		`, managementClusterName))
	}

	// Install Sealed Secrets.
	utils.InstallSealedSecrets()

	var (
		kubeaidConfigForkDir = path.Join(constants.TempDir, "kubeaid-config")
		clusterDir           = path.Join(kubeaidConfigForkDir, "k8s", constants.ParsedConfig.Cluster.ClusterName)

		// Clone the KubeAid config fork locally.
		kubeaidConfigFork = utils.GitCloneRepo(constants.ParsedConfig.Forks.KubeaidConfigForkURL, kubeaidConfigForkDir, gitAuthMethod)
		defaultBranchName = utils.GetDefaultBranchName(kubeaidConfigFork)
	)

	// Prepare the KubeAid config fork.
	{
		// Create and checkout to a new branch, in the kubeaid-config fork.
		workTree, err := kubeaidConfigFork.Worktree()
		if err != nil {
			log.Fatalf("Failed getting kubeaid-config fork worktree : %v", err)
		}
		newBranchName := fmt.Sprintf("kubeaid-%s-%d", constants.ParsedConfig.Cluster.ClusterName, currentTime)
		utils.CreateAndCheckoutToBranch(kubeaidConfigFork, newBranchName, workTree)

		if !ctx.Bool(constants.FlagNameSkipCreateKubeaidConfigFiles) {
			// Create files in the new branch of the KubeAid config fork.
			createKubeaidConfigFiles(clusterDir, gitAuthMethod)

			// Add, commit and push the changes.
			commitMessage := fmt.Sprintf("KubeAid bootstrap setup for argo-cd applications on %s\n", constants.ParsedConfig.Cluster.ClusterName)
			commitHash := utils.AddCommitAndPushChanges(kubeaidConfigFork, workTree, newBranchName, gitAuthMethod, constants.ParsedConfig.Cluster.ClusterName, commitMessage)

			// The user now needs to go ahead and create a PR from the new to the default branch. Then he
			// needs to merge that branch.
			// We can't create the PR for the user, since PRs are not part of the core git lib. They are
			// specific to the git platform the user is on.

			// Wait until the PR gets merged.
			utils.WaitUntilPRMerged(kubeaidConfigFork, defaultBranchName, commitHash, gitAuthMethod, newBranchName)
		}
	}

	// Cloud specific tasks.
	switch {
	case constants.ParsedConfig.Cloud.AWS != nil:
		// The clusterawsadm utility takes the credentials that you set as environment variables and
		// uses them to create a CloudFormation stack in your AWS account with the correct IAM resources.
		// NOTE : This requires admin privileges.
		output, err := utils.ExecuteCommand("clusterawsadm bootstrap iam create-cloudformation-stack")
		//
		// Panic if an error occurs (except regarding the AWS Cloudformation stack already existing).
		if err != nil && !strings.Contains(output, "already exists, updating") {
			log.Fatalf("Command execution failed : %v", output)
		}

	default:
		utils.Unreachable()
	}

	// Provisioning the main cluster.
	{
		// Install and setup ArgoCD.
		argoCDApplicationClient, argoCDApplicationClientCloser := utils.InstallAndSetupArgoCD(clusterDir)
		defer argoCDApplicationClientCloser.Close()

		// Create the capi-cluster / capi-cluster-<customer-id> namespace, where the 'cloud-credentials'
		// Kubernetes Secret will exist.
		capiClusterNamespace := utils.GetCapiClusterNamespace()
		utils.CreateNamespace(capiClusterNamespace)

		// Sync the root, CertManager, Secrets and ClusterAPI ArgoCD Apps one by one.
		argocdAppsToBeSynced := []string{
			"root",
			"cert-manager",
			"secrets",
			"cluster-api",
		}
		for _, argoCDApp := range argocdAppsToBeSynced {
			utils.SyncArgoCDApp(argoCDApplicationClient, argoCDApp, []*argoCDV1Alpha1.SyncOperationResource{})
		}

		// Sync the Infrastructure Provider component and then the whole CAPI Cluster ArgoCD App.
		// TODO : Use ArgoCD sync waves so that we don't need to explicitly sync the Infrastructure
		// Provider component first.
		utils.SyncInfrastructureProvider(argoCDApplicationClient)
		for {
			/*
			  Sometimes, we get this error :

			  one or more objects failed to apply, reason:

			  (1) Internal error occurred: failed calling webhook
			      "default.kubeadmcontrolplane.controlplane.cluster.x-k8s.io": failed to call webhook:
			      Post "https://capi-kubeadm-control-plane-webhook-service.kubeadm-control-plane-system.svc:443/mutate-controlplane-cluster-x-k8s-io-v1beta1-kubeadmcontrolplane?timeout=10s":
			      no endpoints available for service "capi-kubeadm-control-plane-webhook-service"

			  (2) Internal error occurred: failed calling webhook
			      "default.kubeadmconfig.bootstrap.cluster.x-k8s.io": failed to call webhook:
			      Post "https://capi-kubeadm-bootstrap-webhook-service.kubeadm-bootstrap-system.svc:443/mutate-bootstrap-cluster-x-k8s-io-v1beta1-kubeadmconfig?timeout=10s":
			      no endpoints available for service "capi-kubeadm-bootstrap-webhook-service"

			  In that case, we'll retry. Otherwise, we'll fail if the 'error' word exists in the output.
			  If not that means the command execution is successfull.
			*/
			output, _ := utils.ExecuteCommand("argocd app sync argo-cd/capi-cluster")
			if strings.Contains(output, "failed to call webhook") {
				slog.Info("Waiting for kubeadm-control-plane-system and kubeadm-bootstrap-system webhooks to be available....")
				time.Sleep(5 * time.Second)
				continue
			}

			if strings.Contains(output, "error") {
				log.Fatalf("Failed syncing CAPI Cluster ArgoCD App : %v", output)
			}

			break
		}

		// Wait for the main cluster to be provisioned and ready.
		for {
			clusterPhase := utils.ExecuteCommandOrDie(fmt.Sprintf(
				"kubectl get cluster/%s -n %s -o jsonpath=\"{.status.phase}\"",
				constants.ParsedConfig.Cluster.ClusterName, capiClusterNamespace,
			))
			clusterCondition := utils.ExecuteCommandOrDie(fmt.Sprintf(
				"kubectl get cluster/%s -n %s -o jsonpath=\"{.status.conditions[0].type}\"",
				constants.ParsedConfig.Cluster.ClusterName, capiClusterNamespace,
			))
			if clusterPhase == "Provisioned" && clusterCondition == "Ready" {
				break
			}

			slog.Info("Waiting for the main cluster to be provisioned and ready....")
			time.Sleep(time.Minute)
		}

		// Get the provisioned cluster's kubeconifg.
		utils.ExecuteCommandOrDie(fmt.Sprintf(`
      clusterctl get kubeconfig %s -n %s > %s
      chmod 600 %s
    `, constants.ParsedConfig.Cluster.ClusterName, capiClusterNamespace, constants.OutputPathProvisionedClusterKubeconfig, constants.OutputPathProvisionedClusterKubeconfig))
	}

	// Let the provisioned cluster manage itself.
	{
		// Update the KUBECONFIG environment variable's value to the provisioned cluster's kubeconfig.
		os.Setenv(constants.EnvNameKubeconfig, constants.OutputPathProvisionedClusterKubeconfig)

		// Wait for the Kubernetes API server to be reachable and atleast 1 worker node to be
		// initialized.
		for {
			initializedNodeCountAsString, _ := utils.ExecuteCommand(`
        kubectl get nodes --no-headers -o custom-columns=NAME:.metadata.name,TAINTS:.spec.taints \
          | grep -v node.cluster.x-k8s.io/uninitialized \
          | wc -l
      `)
			initializedNodeCountAsString = strings.TrimSpace(initializedNodeCountAsString)

			initializedNodeCount, _ := strconv.Atoi(initializedNodeCountAsString)
			if initializedNodeCount > constants.ParsedConfig.Cloud.AWS.ControlPlaneReplicas {
				break
			}

			slog.Info("Waiting for the provisioned cluster's Kubernetes API server to be reachable and atleast 1 worker node to be initialized....")
			time.Sleep(time.Minute)
		}

		// Install Sealed Secrets.
		utils.InstallSealedSecrets()

		// We need to update the Sealed Secrets in the cluster specific directory in the kubeaid-config
		// fork.
		// Those represent Kubernetes Secrets encyrpted using the private key of the Sealed Secrets
		// controller installed in the K3d management cluster.
		// We need to update them, by encrypting the underlying Kubernetes Secrets using the private
		// key of the Sealed Secrets controller installed in the provisioned main cluster.
		{
			// Checkout to default branch.
			workTree, err := kubeaidConfigFork.Worktree()
			if err != nil {
				log.Fatalf("Failed getting kubeaid-config fork worktree : %v", err)
			}
			if err = workTree.Checkout(&git.CheckoutOptions{
				Branch: plumbing.ReferenceName("refs/heads/" + defaultBranchName),
			}); err != nil {
				log.Fatalf("Failed checking out to branch %s : %v", defaultBranchName, err)
			}

			// Pull changes done by the merged PR.
			if err := workTree.Pull(&git.PullOptions{}); err != nil {
				log.Fatalf("Failed pulling changes in the default branch for kubeaid-config fork", slog.Any("error", err))
			}

			// Update cloud credentials Kubernetes Secret.
			generateCloudCredentialsSealedSecret(clusterDir)
			//
			// Update Kubernetes Secret containing credentials for ArgoCD to access the kubeaid-config
			// fork.
			embeddedTemplateName := constants.TemplateNameKubeaidConfigRepo
			destinationFilePath := path.Join(clusterDir, strings.TrimSuffix(embeddedTemplateName, ".tmpl"))
			createFileFromTemplate(destinationFilePath, embeddedTemplateName, getTemplateValues())

			// Add, commit and push changes.
			commitMessage := fmt.Sprintf("Updating Kubernetes Secrets for cluster %s by encrypting them with private key of Sealed Secrets controller in the provisioned main cluster\n", constants.ParsedConfig.Cluster.ClusterName)
			utils.AddCommitAndPushChanges(kubeaidConfigFork, workTree, defaultBranchName, gitAuthMethod, constants.ParsedConfig.Cluster.ClusterName, commitMessage)
		}

		// Install and setup ArgoCD.
		argoCDApplicationClient, argoCDApplicationClientCloser := utils.InstallAndSetupArgoCD(clusterDir)
		defer argoCDApplicationClientCloser.Close()

		// Create the capi-cluster / capi-cluster-<customer-id> namespace, where the 'cloud-credentials'
		// Kubernetes Secret will exist.
		capiClusterNamespace := utils.GetCapiClusterNamespace()
		utils.CreateNamespace(capiClusterNamespace)

		// Sync the root, cert-manager, sealed-secrets, secrets and cluster-api ArgoCD Apps.
		argocdAppsToBeSynced := []string{
			"root",
			"cert-manager",
			"secrets",
			"cluster-api",
		}
		for _, argoCDApp := range argocdAppsToBeSynced {
			utils.SyncArgoCDApp(argoCDApplicationClient, argoCDApp, []*argoCDV1Alpha1.SyncOperationResource{})
		}

		// Sync the Infrastructure Provider component of the CAPI Cluster ArgoCD App.
		utils.SyncInfrastructureProvider(argoCDApplicationClient)

		skipClusterctlMove := ctx.Bool(constants.FlagNameSkipClusterctlMove)
		if !skipClusterctlMove {
			// Move ClusterAPI manifests to the provisioned cluster.
			utils.ExecuteCommandOrDie(fmt.Sprintf(
				"clusterctl move --kubeconfig %s --namespace %s --to-kubeconfig %s",
				constants.OutputPathManagementClusterKubeconfig, capiClusterNamespace, constants.OutputPathProvisionedClusterKubeconfig,
			))
		}
	}

	slog.Info("Cluster provisioned successfully 🎉🎉 !", slog.String("kubeconfig", constants.OutputPathProvisionedClusterKubeconfig))
	return nil
}
