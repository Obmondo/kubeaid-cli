// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package core

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path"
	"time"

	argoCDV1Alpha1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	"github.com/go-git/go-git/v5/plumbing/transport"
	kubeoneCmd "k8c.io/kubeone/pkg/cmd"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/cluster-api-provider-aws/v2/cmd/clusterawsadm/controller/credentials"
	"sigs.k8s.io/cluster-api-provider-aws/v2/cmd/clusterawsadm/controller/rollout"
	clusterctl "sigs.k8s.io/cluster-api/cmd/clusterctl/client"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Obmondo/kubeaid-cli/pkg/cloud/hetzner"
	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/core/netbird"
	"github.com/Obmondo/kubeaid-cli/pkg/globals"
	"github.com/Obmondo/kubeaid-cli/pkg/utils"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/git"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/kubernetes"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/progress"
)

type BootstrapClusterArgs struct {
	*CreateDevEnvArgs
	SkipClusterctlMove bool
}

func BootstrapCluster(ctx context.Context, args BootstrapClusterArgs) {
	bootstrapStarted := time.Now()
	bar := progress.New("Bootstrapping cluster")
	defer bar.Finish()
	ctx = progress.WithBar(ctx, bar)

	// Workload + NetBird: bail early if the operator isn't on the mesh,
	// so a connectivity failure surfaces here instead of half-a-spinner
	// into provisioning. No-op on VPN / non-NetBird clusters.
	assert.AssertErrNil(ctx, requireOperatorOnNetBird(ctx),
		"NetBird preflight failed")

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

	// Hetzner: kubectl-apply the kube-system/cloud-credentials Secret
	// directly so the HCloud CCM can start before sealed-secrets-
	// controller is up. Mirrors the keycloak-admin pattern; breaks
	// the taint ↔ sealed-secrets ↔ CCM bootstrap cycle. No-op for
	// other cloud providers. It's a single Secret apply — a substep
	// under the still-open "Provisioning main cluster" section, not
	// its own major-step header.
	if globals.CloudProviderName == constants.CloudProviderHetzner {
		releaseCreds := bar.InProgress("Applying HCloud cloud-credentials Secret")
		credsErr := ensureHCloudCredentialsSecret(ctx, mainClusterClient)
		releaseCreds()
		assert.AssertErrNil(ctx, credsErr, "Failed applying HCloud cloud-credentials Secret")
		bar.Substep("Applied HCloud cloud-credentials Secret")
	}

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
	//
	// On Hetzner VPN clusters a chain of apps must come up in a
	// guaranteed order, with gates between:
	//
	//   ccm → traefik → [wait LB IP + operator DNS]
	//       → cert-manager → keycloakx → [wait keycloak-tls Ready,
	//                                     reconcile Keycloak realm + clients]
	//       → netbird → [wait netbird-tls Ready]
	//
	// netbird-management fetches Keycloak's OIDC config over TLS and
	// authenticates as its OIDC client, so keycloak's cert must be Ready
	// and the realm + clients must exist before netbird syncs; cert-manager
	// must be up before either Ingress cert can issue; and a Synced
	// ArgoCD App only means its manifests were applied, not that the
	// cert was issued. orderedApps makes that sequence explicit instead
	// of leaning on the alphabetical order ArgoCD's List returns.
	bar.Describe("Syncing ArgoCD applications")
	var orderedApps []kubernetes.AppSyncStep
	if config.VPNClusterEnabled() && globals.CloudProviderName == constants.CloudProviderHetzner {
		// ccm-hcloud manages LoadBalancers for HCloud nodes and must be up before
		// traefik so the ingress LB Service gets an IP. ccm-hetzner (bare-metal /
		// hybrid) follows; it doesn't own LBs so traefik-ordering is less critical
		// but sync order is still declared to keep the sequence deterministic.
		// WaitForIngressLBDNS then waits for the operator to point DNS at the IP.
		if config.UsingHCloud() {
			orderedApps = append(orderedApps,
				kubernetes.AppSyncStep{Name: constants.ArgoCDAppCCMHCloud})
		}
		if config.UsingHetznerBareMetal() {
			orderedApps = append(orderedApps,
				kubernetes.AppSyncStep{Name: constants.ArgoCDAppCCMHetzner})
		}
		orderedApps = append(orderedApps, kubernetes.AppSyncStep{
			Name: constants.ArgoCDAppTraefik,
			AfterSync: func(ctx context.Context) error {
				return hetzner.WaitForIngressLBDNS(ctx, mainClusterClient)
			},
		})
	}
	if config.VPNClusterEnabled() {
		// cert-manager must be running before keycloakx / netbird sync
		// so it can issue their Ingress certs. After each of those
		// syncs, gate on the Certificate object itself being Ready —
		// a failed cert otherwise surfaces much later as a cryptic
		// netbird-management x509 crashloop. Cert names match the
		// tls.secretName rendered into values-keycloakx / values-netbird.
		orderedApps = append(orderedApps,
			kubernetes.AppSyncStep{Name: constants.ArgoCDAppCertManager})
		// cloudnative-pg installs the postgresql.cnpg.io/v1 Cluster CRD
		// that the keycloakx (managed mode → keycloak-pgsql) and netbird
		// (always → netbird-pgsql) charts both materialise during sync.
		// Without this step those apps fail with "could not find version
		// v1 of postgresql.cnpg.io/Cluster" until the generic remaining-
		// apps loop happens to sync cnpg, which can be much later.
		orderedApps = append(orderedApps,
			kubernetes.AppSyncStep{Name: constants.ArgoCDAppCloudNativePG})
		if config.ManagedKeycloakEnabled() {
			orderedApps = append(orderedApps, kubernetes.AppSyncStep{
				Name:      constants.ArgoCDAppKeycloakx,
				AfterSync: keycloakxAfterSync(mainClusterClient),
			})
		}
		orderedApps = append(orderedApps, kubernetes.AppSyncStep{
			Name:      constants.ArgoCDAppNetbird,
			AfterSync: netbirdAfterSync(mainClusterClient),
		})
	}
	err = kubernetes.SyncAllArgoCDApps(ctx, args.SkipMonitoringSetup, orderedApps)
	assert.AssertErrNil(ctx, err, "Failed syncing all ArgoCD apps")

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

	// Read the Keycloak admin password while kube-apiserver is still
	// publicly reachable. After the disable step below, the kubeconfig's
	// apiserver URL (e.g. api.vpn.obmondo.com) resolves to a now-dead
	// public IP and the operator's machine — unless it's on the NetBird
	// mesh — can't fetch in-cluster Secrets at all. Stashing the value
	// here lets the next-steps panel print the actual password instead
	// of telling the operator to ssh through the NAT gateway just to
	// run `kubectl get secret`. Empty string when no managed Keycloak
	// or when the read fails (panel falls back to the kubectl command).
	keycloakAdminPassword := readKeycloakAdminPasswordForPanel(ctx, mainClusterClient)

	// Verify the user-facing endpoints of a managed-Keycloak VPN
	// cluster (Keycloak realm, NetBird dashboard, NetBird Mgmt API)
	// before locking down the control-plane LB. Catches the class of
	// "pods are Ready but SSO is broken" bugs that otherwise only
	// surface on first user login — long after kubeaid-cli has exited
	// and DisableControlPlaneLBPublicInterface has cut off the easy
	// debug path. No-op on non-VPN clusters.
	assert.AssertErrNil(ctx,
		verifyVPNClusterEndpoints(ctx),
		"VPN cluster endpoint verification failed",
	)

	// Interactive prompts below need clean stdout.
	bar.Finish()

	// NetBird API-key gate — see netbird.AwaitOperatorToken. When the operator
	// defers, skip lockdown + the LB disable: without a mesh key they'd have
	// no path back to kube-apiserver.
	proceedWithLockdown, netBirdErr := netbird.AwaitOperatorToken(ctx, mainClusterClient, keycloakAdminPassword)
	assert.AssertErrNil(ctx, netBirdErr, "Failed handling the NetBird operator API-key gate")

	if proceedWithLockdown {
		// Host-firewall lockdown runs BEFORE the LB public-interface disable
		// below: every step inside it needs live kube-apiserver access — the
		// IsClusterctlMoveExecuted gate check (a live Get), listing node public
		// IPs, and the CCNP server-side apply — and disabling the control-plane
		// LB public interface severs that access on a VPN cluster (the operator's
		// machine isn't on the NetBird mesh yet). Self-gates to Hetzner
		// bare-metal post-pivot; the operator can decline.
		lockdownInBootstrap(ctx, mainClusterClient, gitAuthMethod)

		// Disable the control-plane LB public interface LAST — it's the final
		// step that severs the operator's public path to kube-apiserver, so it
		// must run after every CLI→cluster operation above, lockdown included.
		// No-op on non-VPN clusters and non-Hetzner providers.
		if globals.CloudProviderName == constants.CloudProviderHetzner {
			hetznerCloudProvider, ok := globals.CloudProvider.(*hetzner.Hetzner)
			assert.Assert(ctx, ok, "Failed type-casting globals.CloudProvider to *hetzner.Hetzner")
			assert.AssertErrNil(ctx,
				hetznerCloudProvider.DisableControlPlaneLBPublicInterface(ctx),
				"Failed disabling control-plane LB public interface",
			)
		}
	}

	slog.InfoContext(ctx, "Main cluster has been bootstrapped successfully 🎊")

	// Elapsed time only renders on the success path — a Ctrl+C or
	// assert.AssertErrNil bail-out short-circuits before this call,
	// so the "Bootstrap complete in …" header never lies about a
	// run that didn't actually complete.
	printPostBootstrapNextSteps(keycloakAdminPassword, time.Since(bootstrapStarted))
}

// readKeycloakAdminPasswordForPanel reads the Keycloak admin password
// for the post-bootstrap next-steps panel, or returns "" when there's
// no managed Keycloak to surface or the read failed. Empty string is
// also the panel's signal to fall back to the kubectl-fetch command.
//
// Pulled out so the call site reads as a single line and the warning
// log doesn't clutter the bootstrap-finalize flow.
func readKeycloakAdminPasswordForPanel(
	ctx context.Context,
	clusterClient client.Client,
) string {
	if !config.VPNClusterEnabled() || !config.ManagedKeycloakEnabled() {
		return ""
	}
	password, err := readSecretValue(ctx, clusterClient,
		constants.NamespaceKeycloak,
		constants.SecretNameKeycloakAdmin,
		constants.SecretKeyKeycloakPassword,
	)
	if err != nil {
		slog.WarnContext(ctx,
			"Could not read Keycloak admin password for the next-steps panel; "+
				"the panel will print the kubectl-fetch command as a fallback",
			slog.Any("err", err),
		)
		return ""
	}
	return password
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

	// Closes the well-documented gap between CAPI's Cluster.Phase=
	// Provisioned+Ready (which WaitForMainClusterToBeProvisioned above
	// gates on) and the cluster actually being able to schedule pods.
	// CAPI's predicate flips True the moment static control-plane pods
	// respond on the apiserver endpoint — it has no opinion on whether
	// the CNI is installed and a Node can run workloads. Without this
	// extra gate, a postKubeadm cilium install rollback used to slip
	// past us silently and surface much later as SealedSecrets /
	// ArgoCD App sync failures with workload pods stuck
	// ContainerCreating. Failing fast here points the operator at the
	// actual problem (CNI not Ready on the CP Node).
	bar := progress.FromCtx(ctx)
	releaseNet := bar.InProgress("Waiting for control-plane Node networking to be ready")
	err = kubernetes.WaitForCPNodesNetworkingReady(ctx, provisionedClusterClient)
	releaseNet()
	assert.AssertErrNil(ctx, err, "Failed waiting for control-plane Node networking to be ready")
	bar.Substep("Control-plane Node networking ready")

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

	// Gate the workload installs below on a settled control plane: a re-run
	// can land mid control-plane rollout, and installing during that
	// apiserver/etcd churn can leave a Helm release stuck "failed".
	if kubernetes.UsingClusterAPI() {
		// KCP lives in the management cluster pre-pivot, the provisioned
		// cluster after clusterctl move (mgmt k3d may be gone by then).
		kcpOwnerClient := provisionedClusterClient
		if !kubernetes.IsClusterctlMoveExecuted(ctx) {
			mgmtKubeconfigPath, mgmtErr := kubernetes.GetManagementClusterKubeconfigPath(ctx)
			assert.AssertErrNil(ctx, mgmtErr, "Failed getting management cluster kubeconfig path")

			mgmtClient, mgmtErr := kubernetes.CreateKubernetesClient(ctx, mgmtKubeconfigPath)
			assert.AssertErrNil(ctx, mgmtErr,
				"Failed constructing management cluster client for control-plane rollout gate")

			kcpOwnerClient = mgmtClient
		}

		err = kubernetes.WaitForControlPlaneRolloutComplete(ctx, kcpOwnerClient)
		assert.AssertErrNil(ctx, err, "Failed waiting for control-plane rollout to settle")
		bar.Substep("Control-plane rollout settled")
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
		pivotCluster(ctx, provisionedClusterClient)
	}

	// Sync cluster-autoscaler on AWS or Azure workload clusters.
	// Skip Hetzner (chart wiring not in place), bare-metal (no
	// scaling), Local (k3d), and any VPN cluster (operator-fixed).
	if !config.VPNClusterEnabled() &&
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
	// Predicate inside WaitForMainClusterToBeProvisioned now requires
	// Cluster Provisioned+Ready AND at least one CP Machine Running
	// with a Node AND (when workers exist) at least one worker Machine
	// Running with a Node. Once we're here, the API is reachable and
	// a schedulable untainted node exists — enough for the CNI helm
	// install + workload-cluster ArgoCD app sync that follow.
	bar.Substep("Main cluster reachable (CP + worker Machine joined)")

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

// pivotCluster runs `clusterctl move` to hand off CAPI ownership from
// the management cluster to the just-provisioned main cluster.
// mainClusterClient is the workload-side client used for the
// pre-pivot Node-table render (purely informational); the actual
// move walks the management cluster via clusterctlClient.
func pivotCluster(ctx context.Context, mainClusterClient client.Client) {
	bar := progress.FromCtx(ctx)
	capiClusterNamespace := kubernetes.GetCapiClusterNamespace()

	pivotMgmtKubeconfig, pivotErr := kubernetes.GetManagementClusterKubeconfigPath(ctx)
	assert.AssertErrNil(ctx, pivotErr, "Failed getting management cluster kubeconfig path")

	managementClusterClient, mgmtClientErr := kubernetes.CreateKubernetesClient(ctx, pivotMgmtKubeconfig)
	assert.AssertErrNil(ctx, mgmtClientErr,
		"Failed constructing management cluster client for the pre-pivot Machine wait")

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

	// Wait until every Machine in capi-cluster is Phase=Running with a
	// Node registered. clusterctl move refuses to start if any Machine
	// is still mid-provision (its predicate is the same status.nodeRef
	// check). The earlier WaitForMainClusterToBeProvisioned cleared the
	// *initial* provisioning, but SetupCluster has since run multi-
	// minute ArgoCD syncs that can flip the KCP spec (e.g. chart upgrade
	// between attempts) and kick off a control-plane rolling update —
	// leaving us at "N-1 of N Machines Running, one fresh Machine still
	// joining" when we arrive here. Without this wait, that exact case
	// hard-fails the pivot.
	//
	// On success the wait swaps its live Machine-status table for a
	// `kubectl get nodes`-style table from mainClusterClient — the
	// operator's pre-pivot audit trail.
	waitErr := kubernetes.WaitForAllMachinesRunning(ctx, managementClusterClient, mainClusterClient)
	assert.AssertErrNil(ctx, waitErr,
		"Timed out waiting for Machines to be Running before clusterctl move")

	// Pause the ClusterAPI Infrastructure Provider in the management cluster,
	// and move the ClusterAPI manifests to the main cluster. They will be processed by the main
	// cluster's Infrastructure Provider.

	capiCLI, err := clusterctl.New(ctx, "")
	assert.AssertErrNil(ctx, err, "Failed constructing clusterctl client")

	releasePivot := bar.InProgress("Pivoting ClusterAPI to main cluster")
	err = capiCLI.Move(ctx, clusterctl.MoveOptions{
		FromKubeconfig: clusterctl.Kubeconfig{
			Path: pivotMgmtKubeconfig,
		},

		ToKubeconfig: clusterctl.Kubeconfig{
			Path: constants.OutputPathMainClusterKubeconfig,
		},

		Namespace: capiClusterNamespace,
	})
	releasePivot()
	assert.AssertErrNil(ctx, err, "Failed pivoting the cluster by executing 'clusterctl move'")
	slog.InfoContext(ctx, "Pivoted the cluster by executing 'clusterctl move'")
	bar.Substep("Pivoted ClusterAPI to main cluster")
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

// waitForAppCertificate returns a SyncAllArgoCDApps after-sync hook
// that blocks until the named cert-manager Certificate is Ready,
// surfaced as a "↻ Waiting for <label>" / "✓ <label> issued" sub-step
// pair. label is operator-facing (e.g. "keycloak TLS certificate").
func waitForAppCertificate(
	clusterClient client.Client,
	label, namespace, name string,
) func(context.Context) error {
	return func(ctx context.Context) error {
		bar := progress.FromCtx(ctx)
		release := bar.InProgress("Waiting for " + label)
		err := kubernetes.WaitForCertificatesReady(ctx, clusterClient,
			[]types.NamespacedName{{Namespace: namespace, Name: name}})
		release()
		if err != nil {
			return err
		}
		bar.Substep(label + " issued")
		return nil
	}
}

// keycloakxAfterSync is the keycloakx AppSyncStep's after-sync hook:
// wait for the keycloak-tls Certificate to be Ready, then log into
// Keycloak and reconcile NetBird's realm + the netbird / kubernetes
// OIDC clients. Running the reconcile here — before the netbird app
// syncs — means netbird-management comes up against OIDC clients that
// already exist, instead of crashlooping on OIDC discovery until a
// post-sync pass creates them.
func keycloakxAfterSync(clusterClient client.Client) func(context.Context) error {
	return func(ctx context.Context) error {
		certWait := waitForAppCertificate(clusterClient,
			"keycloak TLS certificate", constants.NamespaceKeycloak, "keycloak-tls")
		if err := certWait(ctx); err != nil {
			return err
		}

		bar := progress.FromCtx(ctx)
		release := bar.InProgress("Logging into Keycloak admin and reconciling realm + clients")
		err := reconcileNetBirdInKeycloak(ctx, clusterClient)
		release()
		if err != nil {
			return err
		}
		bar.Substep("Reconciled NetBird's resources in Keycloak")
		return nil
	}
}

// netbirdAfterSync is netbird's AppSyncStep after-sync hook:
//
//  1. Patch netbird's Secret with the postgres DSN built from CNPG's
//     auto-generated netbird-pgsql-app credentials. Done first
//     because the patch is quick (a Get + an Update on two Secrets,
//     plus a short poll for CNPG to finish provisioning) and lets
//     netbird-management start consuming postgres straight away
//     instead of crashlooping until a separate post-sync step lands.
//
//  2. Wait for the netbird-tls Certificate to be Ready, so callers
//     that depend on the public NetBird URL (NetBird clients, the
//     dashboard) have a valid cert by the time bootstrap returns.
//
// Co-locating netbird's wiring with its sync step replaces a
// trailing "Patching NetBird Secret" block that used to run after
// the entire sync loop — moving it here means NetBird Mgmt is
// usable sooner and netbird's setup is in one place.
func netbirdAfterSync(clusterClient client.Client) func(context.Context) error {
	return func(ctx context.Context) error {
		bar := progress.FromCtx(ctx)
		release := bar.InProgress("Patching netbird Secret with CNPG-generated postgres DSN")
		err := netbird.WaitAndPatchPostgresDSN(ctx, clusterClient)
		release()
		if err != nil {
			return err
		}
		bar.Substep("Patched netbird Secret with postgres DSN")

		certWait := waitForAppCertificate(clusterClient,
			"netbird TLS certificate", constants.NamespaceNetBird, "netbird-tls")
		return certWait(ctx)
	}
}
