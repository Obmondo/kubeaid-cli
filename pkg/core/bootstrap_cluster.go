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
	kubeoneCmd "k8c.io/kubeone/pkg/cmd"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/cluster-api-provider-aws/v2/cmd/clusterawsadm/controller/credentials"
	"sigs.k8s.io/cluster-api-provider-aws/v2/cmd/clusterawsadm/controller/rollout"
	"sigs.k8s.io/cluster-api/cmd/clusterctl/client"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud/hetzner"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config/parser"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/git"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/kubernetes"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/progress"
)

type BootstrapClusterArgs struct {
	*CreateDevEnvArgs
	SkipClusterctlMove bool
}

func BootstrapCluster(ctx context.Context, args BootstrapClusterArgs) {
	bar := progress.New("Bootstrapping cluster")
	defer bar.Finish()
	ctx = progress.WithBar(ctx, bar)

	// Workload-cluster banner: names the OIDC client the operator
	// must have pre-created in their Keycloak realm, or warns about
	// the admin.conf fallback when no Keycloak is referenced. No-op
	// on VPN clusters.
	printWorkloadOIDCBanner(ctx)

	// Pre-flight: when the user opted into OIDC, probe Keycloak's
	// discovery endpoint before any infrastructure is touched.
	// Catches typo'd issuer URLs / unreachable Keycloak before
	// Hetzner VMs (etc.) are provisioned.
	//
	// Skipped entirely (no spinner step) when:
	//   - apiServer.oidc isn't set, OR
	//   - cluster.keycloak.mode == managed (the issuer is provisioned
	//     by THIS bootstrap run; probing now would NXDOMAIN /
	//     TLS-mismatch). ValidateOIDCDiscovery has the same internal
	//     skip but the spinner step would still flash.
	if shouldValidateOIDCNow() {
		bar.Describe("Validating OIDC issuer")
		assert.AssertErrNil(ctx, parser.ValidateOIDCDiscovery(ctx),
			"OIDC issuer validation failed")
	}

	// When using Hetzner, ensure that prerequisite infrastructure is provisioned.
	// NOTE : Though HCloud has an official Terraform provider which can be imported into a
	//        CrossPlane provider, Hetzner Bare Metal doesn't have any. So, we can't use CrossPlane
	//        as of now.
	if globals.CloudProviderName == constants.CloudProviderHetzner {
		bar.Describe("Provisioning Hetzner infrastructure")

		hetznerCloudProvider, ok := globals.CloudProvider.(*hetzner.Hetzner)
		assert.Assert(ctx, ok, "Failed type-casting globals.CloudProvider to *hetzner.Hetzner")

		assert.AssertErrNil(ctx,
			hetznerCloudProvider.ProvisionPrerequisiteInfrastructure(ctx),
			"Failed provisioning prerequisite Hetzner infrastructure",
		)
	}

	// Detect git authentication method. Fast (no network), so no
	// separate spinner step — folded into "Creating management
	// cluster" which is the first step that actually uses it.
	gitAuthMethod := git.GetGitAuthMethod(ctx)

	// Create and setup the management cluster. The capi-cluster sync
	// is folded into SetupCluster so it runs before the
	// "Management cluster ready" box, not after it.
	bar.Describe("Creating management cluster")
	CreateDevEnv(ctx, args.CreateDevEnvArgs)

	bar.Describe("Provisioning main cluster")
	provisionAndSetupMainCluster(ctx, ProvisionAndSetupMainClusterArgs{
		BootstrapClusterArgs: &args,
		GitAuthMethod:        gitAuthMethod,
	})

	// Construct main cluster client.
	mainClusterClient, err := kubernetes.CreateKubernetesClient(ctx,
		utils.MustGetEnv(constants.EnvNameKubeconfig))
	assert.AssertErrNil(ctx, err, "Failed constructing Kubernetes cluster client")

	// Setup Disaster Recovery, if the user wants.
	if config.ParsedGeneralConfig.Cloud.DisasterRecovery != nil && globals.CloudProvider != nil {
		bar.Describe("Setting up disaster recovery")
		err = globals.CloudProvider.SetupDisasterRecovery(ctx)
		assert.AssertErrNil(ctx, err, "Failed setting up disaster recovery")
	}

	// When this is part of a disaster recovery, we don't want to progress any further here,
	// but instead, restore the latest backup.
	if args.IsPartOfDisasterRecovery {
		return
	}

	// Sync all ArgoCD Apps.
	bar.Describe("Syncing ArgoCD applications")
	err = kubernetes.SyncAllArgoCDApps(ctx, args.SkipMonitoringSetup)
	assert.AssertErrNil(ctx, err, "Failed syncing all ArgoCD apps")

	// VPN clusters on Hetzner: Traefik is up by now and CCM has
	// allocated a public LB IP for it. Pause and have the operator
	// point keycloak.dns / netbird.dns / stun.dns / turn.dns at
	// that IP — cert-manager's ACME challenges retry with backoff,
	// so the next retry succeeds once DNS resolves correctly.
	if vpnClusterEnabled() && globals.CloudProviderName == constants.CloudProviderHetzner {
		bar.Describe("Waiting for ingress-LB DNS")
		assert.AssertErrNil(ctx,
			hetzner.WaitForIngressLBDNS(ctx, mainClusterClient),
			"Failed waiting for ingress-LB DNS",
		)
	}

	// Managed Keycloak only: kubeaid-cli logs into the in-cluster
	// Keycloak via the admin secret and reconciles the realm +
	// NetBird OIDC clients. External-mode operators handle their
	// own Keycloak setup (per the realm-prerequisites doc in
	// kubeaid/argocd-helm-charts/netbird/README.md).
	if managedKeycloakEnabled() {
		bar.Describe("Reconciling NetBird's resources in Keycloak")
		releaseRealm := bar.InProgress("Logging into Keycloak admin and reconciling realm + clients")
		err := reconcileNetBirdInKeycloak(ctx, mainClusterClient)
		releaseRealm()
		assert.AssertErrNil(ctx, err, "Failed reconciling NetBird in Keycloak")
		bar.Substep("Reconciled NetBird's resources in Keycloak")
	}

	// Both Keycloak modes: NetBird Mgmt's postgresDSN can only be
	// filled in once CNPG has created the netbird-pgsql-app Secret
	// in-cluster, so this patch is mode-independent.
	if vpnClusterEnabled() {
		bar.Describe("Patching NetBird Secret with CNPG-generated postgres DSN")
		releaseDSN := bar.InProgress("Reading CNPG app credentials and patching netbird Secret")
		err := patchNetBirdPostgresDSN(ctx, mainClusterClient)
		releaseDSN()
		assert.AssertErrNil(ctx, err, "Failed patching NetBird postgres DSN")
		bar.Substep("Patched NetBird Secret with postgres DSN")
	}

	// When we have setup Disaster Recovery,
	// trigger the first Velero and SealedSecret backups.
	if config.ParsedGeneralConfig.Cloud.DisasterRecovery != nil && globals.CloudProvider != nil {
		bar.Describe("Creating initial backups")

		// Create the first Velero backup.
		releaseVelero := bar.InProgress("Creating initial Velero backup")
		veleroErr := kubernetes.CreateBackup(ctx, "init", mainClusterClient)
		releaseVelero()
		if veleroErr != nil {
			assert.AssertErrNil(ctx, veleroErr, "Failed creating initial Velero backup")
		}
		bar.Substep("Created initial Velero backup")

		// Create first Sealed Secrets backup.
		releaseSS := bar.InProgress("Triggering Sealed Secrets backup CRONJob")
		err = kubernetes.TriggerCRONJob(ctx,
			types.NamespacedName{
				Name:      constants.CRONJobNameBackupSealedSecrets,
				Namespace: constants.NamespaceSealedSecrets,
			},
			mainClusterClient,
		)
		releaseSS()
		assert.AssertErrNil(ctx, err, "Failed triggering Sealed Secrets backup CRONJob")
		bar.Substep("Triggered Sealed Secrets backup CRONJob")
	}

	if globals.CloudProviderName == constants.CloudProviderHetzner {
		hetznerCloudProvider, ok := globals.CloudProvider.(*hetzner.Hetzner)
		assert.Assert(ctx, ok, "Failed type-casting globals.CloudProvider to *hetzner.Hetzner")
		assert.AssertErrNil(ctx,
			hetznerCloudProvider.DisableControlPlaneLBPublicInterface(ctx),
			"Failed disabling control-plane LB public interface",
		)
	}

	bar.Finish()
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

		// Close management cluster's ArgoCD application client.
		_ = globals.ArgoCDApplicationClientCloser.Close()
	}

	// Update the KUBECONFIG environment variable's value to the provisioned cluster's kubeconfig.
	utils.MustSetEnv(constants.EnvNameKubeconfig, constants.OutputPathMainClusterKubeconfig)
	provisionedClusterClient, err := kubernetes.CreateKubernetesClient(ctx,
		constants.OutputPathMainClusterKubeconfig,
	)
	assert.AssertErrNil(ctx, err, "Failed constructing Kubernetes cluster client")

	// Ensure that application workloads can be scheduled.
	switch kubernetes.IsNodeGroupCountZero(ctx) {
	// When there are 0 node-groups, then we need to remove the NoSchedule taint from the master
	// nodes.
	case true:
		err = kubernetes.RemoveNoScheduleTaintsFromMasterNodes(ctx, provisionedClusterClient)
		assert.AssertErrNil(ctx, err, "Failed removing no-schedule taints from master nodes")

	// Otherwise, wait for atleast 1 worker node to be initialized.
	default:
		err = kubernetes.WaitForMainClusterToBeReady(ctx, provisionedClusterClient)
		assert.AssertErrNil(ctx, err, "Failed waiting for atleast 1 worker node to be initialized")
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

	bar := progress.FromCtx(ctx)

	// Sync cluster-autoscaler on AWS or Azure workload clusters.
	// Skip Hetzner (chart wiring not in place), bare-metal (no
	// scaling), Local (k3d), and any VPN cluster (operator-fixed).
	if !vpnClusterEnabled() &&
		(globals.CloudProviderName == constants.CloudProviderAWS ||
			globals.CloudProviderName == constants.CloudProviderAzure) {
		releaseAuto := bar.InProgress("Syncing cluster-autoscaler ArgoCD app")
		err = kubernetes.SyncArgoCDApp(ctx, constants.ArgoCDAppClusterAutoscaler,
			[]*argoCDV1Alpha1.SyncOperationResource{},
		)
		releaseAuto()
		assert.AssertErrNil(ctx, err, "Failed syncing cluster-autoscaler ArgoCD app")
		bar.Substep("Synced cluster-autoscaler ArgoCD app")
	}

	// Sync the external-snapshotter ArgoCD App,
	// if not using Hetzner (since currently we don't support setting up disaster recovery for
	// Hetzner 🥴).
	if globals.CloudProviderName != constants.CloudProviderHetzner {
		releaseSnap := bar.InProgress("Syncing external-snapshotter ArgoCD app")
		err = kubernetes.SyncArgoCDApp(ctx, constants.ArgoCDExternalSnapshotter,
			[]*argoCDV1Alpha1.SyncOperationResource{},
		)
		releaseSnap()
		assert.AssertErrNil(ctx, err, "Failed syncing external-snapshotter ArgoCD app")
		bar.Substep("Synced external-snapshotter ArgoCD app")
	}
}

func provisionMainClusterUsingClusterAPI(ctx context.Context) {
	bar := progress.FromCtx(ctx)

	// Determine whether 'clusterctl move' has been executed or not.
	// If yes, then we don't need to do anything.
	isClusterctlMoveExecuted := kubernetes.IsClusterctlMoveExecuted(ctx)
	if isClusterctlMoveExecuted {
		return
	}

	mgmtKubeconfig, mgmtErr := kubernetes.GetManagementClusterKubeconfigPath(ctx)
	assert.AssertErrNil(ctx, mgmtErr, "Failed getting management cluster kubeconfig path")

	managementClusterClient, clientErr := kubernetes.CreateKubernetesClient(ctx, mgmtKubeconfig)
	assert.AssertErrNil(ctx, clientErr, "Failed constructing Kubernetes cluster client")

	if config.UsingHetznerBareMetal() {
		// When the control-plane is in Hetzner Bare Metal, and we're using a Failover IP,
		// we need to make the Failover IP point to the 'init master node'.
		// 'init master node' is the very first master node, where 'kubeadm init' is executed.
		if config.ControlPlaneInHetznerBareMetal() &&
			config.ParsedGeneralConfig.Cloud.Hetzner.ControlPlane.BareMetal.Endpoint.IsFailoverIP {

			hetznerCloudProvider, ok := globals.CloudProvider.(*hetzner.Hetzner)
			assert.Assert(ctx, ok, "Failed casting CloudProvider to Hetzner cloud-provider")

			assert.AssertErrNil(ctx, hetznerCloudProvider.PointFailoverIPToInitMasterNode(ctx),
				"Failed pointing failover IP to the init master node")
		}
	}

	// Wait for the main cluster to be provisioned. The wait function
	// owns the screen for its duration — pauses our spinner, renders a
	// live status table, leaves the final tick in scrollback as the
	// audit trail. So no bar.InProgress wrap here.
	if err := kubernetes.WaitForMainClusterToBeProvisioned(ctx, managementClusterClient); err != nil {
		assert.AssertErrNil(ctx, err, "Failed waiting for the main cluster to be provisioned")
	}
	bar.Substep("Main cluster Machines provisioned")

	// Save kubeconfig locally.
	if err := kubernetes.SaveProvisionedClusterKubeconfig(ctx, managementClusterClient); err != nil {
		assert.AssertErrNil(ctx, err, "Failed saving provisioned cluster kubeconfig")
	}
	bar.Substep("Saved provisioned cluster kubeconfig")

	slog.InfoContext(ctx,
		"Main cluster has been provisioned successfully 🎉🎉 !",
		slog.String("kubeconfig", constants.OutputPathMainClusterKubeconfig),
	)
}

func pivotCluster(ctx context.Context) {
	bar := progress.FromCtx(ctx)
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

	pivotMgmtKubeconfig, pivotErr := kubernetes.GetManagementClusterKubeconfigPath(ctx)
	assert.AssertErrNil(ctx, pivotErr, "Failed getting management cluster kubeconfig path")

	releasePivot := bar.InProgress("Running clusterctl move (mgmt → main)")
	err = clusterctlClient.Move(ctx, client.MoveOptions{
		FromKubeconfig: client.Kubeconfig{
			Path: pivotMgmtKubeconfig,
		},

		ToKubeconfig: client.Kubeconfig{
			Path: constants.OutputPathMainClusterKubeconfig,
		},

		Namespace: capiClusterNamespace,
	})
	releasePivot()
	assert.AssertErrNil(ctx, err, "Failed pivoting the cluster by executing 'clusterctl move'")
	slog.InfoContext(ctx, "Pivoted the cluster by executing 'clusterctl move'")
	bar.Substep("Pivoted ClusterAPI to main cluster (clusterctl move)")
}

func provisionMainClusterUsingKubeOne(ctx context.Context) {
	mainClusterName := config.ParsedGeneralConfig.Cluster.Name

	kubeoneDir := path.Join(utils.GetClusterDir(), "kubeone")

	slog.InfoContext(ctx, "Provisioning main cluster using Kubermatic KubeOne")

	// Run "kubeone apply".
	kubeoneCmd := kubeoneCmd.NewRoot()
	kubeoneCmd.SetArgs([]string{
		"apply",
		"--manifest", fmt.Sprintf("%s/kubeone-cluster.yaml", kubeoneDir),
		"--auto-approve",

		/*
			It's common to have Docker installed in the servers. And installing Docker, installs
			ContainerD.
			REFER : https://docs.docker.com/engine/install/ubuntu/#install-using-the-repository.

			NOTE : The Docker APT repository must be added using commands which KubeOne use :
			       https://github.com/kubermatic/kubeone/blob/225825f44bf38f4c5eca33c76343aed9319413ca/pkg/scripts/render.go#L55.

			       Otherwise, if we use the commands specified in Docker's documentation website,
			       /etc/apt/sources.list.d/docker.sources and /etc/apt/keyrings/docker.asc will collide
			       with /etc/apt/sources.list.d/docker.list and /etc/apt/keyrings/docker.gpg created
			       by KubeOne.

			When initializing the node, KubeOne also tries to install ContainerD in there.
			Now, the issue is : KubeOne relies on a specific version of ContainerD, which is pretty old.
			REFER : https://github.com/kubermatic/kubeone/blob/225825f44bf38f4c5eca33c76343aed9319413ca/pkg/scripts/render.go#L80.

			So, it'll most most likely try to downgrade the ContainerD APT package. And to downgrade
			an APT package you need to use the "--allow-downgrade" flag, which is enabled by using this
			"--force-install" flag.
		*/
		"--force-install",
	})
	err := kubeoneCmd.ExecuteContext(ctx)
	assert.AssertErrNil(ctx, err,
		"Failed initializing Kubernetes cluster using KubeOne")

	// KubeOne backups the main cluster's PKI infrastructure in a .tar.gz file locally.
	// We don't need it.
	err = os.Remove(fmt.Sprintf("%s/%s.tar.gz", kubeoneDir, mainClusterName))
	assert.AssertErrNil(ctx, err,
		"Failed deleting main cluster's PKI infrastructure backup")

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
	err = utils.MoveFile(
		kubeoneGeneratedKubeconfigFilePath, constants.OutputPathMainClusterKubeconfig,
	)
	assert.AssertErrNil(ctx, err, "Failed moving KubeOne-generated kubeconfig")

	slog.InfoContext(ctx,
		"Main cluster has been provisioned successfully 🎉🎉 !",
		slog.String("kubeconfig", constants.OutputPathMainClusterKubeconfig),
	)
}
