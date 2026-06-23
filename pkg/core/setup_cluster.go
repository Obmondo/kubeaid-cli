// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package core

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	argoCDV1Alpha1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	"github.com/charmbracelet/lipgloss"
	"github.com/go-git/go-git/v5/plumbing/transport"
	coreV1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	clusterctlV1 "sigs.k8s.io/cluster-api/cmd/clusterctl/api/v1alpha3"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Obmondo/kubeaid-cli/pkg/cloud/azure"
	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/globals"
	"github.com/Obmondo/kubeaid-cli/pkg/utils"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/assert"
	gitUtils "github.com/Obmondo/kubeaid-cli/pkg/utils/git"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/kubernetes"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/logger"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/progress"
)

type SetupClusterArgs struct {
	*CreateDevEnvArgs

	ClusterType   string
	ClusterClient client.Client

	GitAuthMethod transport.AuthMethod
}

func SetupCluster(ctx context.Context, args SetupClusterArgs) {
	slog.InfoContext(ctx, "Setting up cluster....", slog.String("cluster-type", args.ClusterType))

	bar := progress.FromCtx(ctx)

	{
		// Clone the KubeAid fork locally (if not already cloned).
		// PinnedRef tells CloneRepo to skip the default-branch fetch
		// dance on re-runs — kubeaid-cli only ever HardResetRepoToRef
		// against this fixed version, never walks default-branch
		// history. One narrow fetch instead of pulling every ref + tag
		// per re-run.
		kubeAidRepo := gitUtils.CloneRepo(ctx,
			config.ParsedGeneralConfig.Forks.KubeaidFork.URL,
			args.GitAuthMethod,
			gitUtils.CloneRepoOptions{
				PinnedRef: config.ParsedGeneralConfig.Forks.KubeaidFork.Version,
			},
		)

		// Hard reset to the KubeAid git ref (tag / branch) from the general config.
		gitUtils.HardResetRepoToRef(ctx,
			kubeAidRepo,
			config.ParsedGeneralConfig.Forks.KubeaidFork.Version,
		)
	}

	// Create required namespaces before syncing all the ArgoCD Apps.
	// Otherwise, some syncing of ArgoCD Apps might fail.
	// For e.g. : syncing of the kube-prometheus ArgoCD App fails if the obmondo namespace doesn't
	// exist.
	namespacesToBeCreated := []string{
		"crossplane",
		"obmondo",
		"traefik",
		"system",
		// sealed-secrets is created up front (not via Helm's
		// CreateNamespace=true) so we can pre-seed the controller's key
		// material before the chart syncs. The Helm install below is a
		// no-op against an existing ns.
		constants.NamespaceSealedSecrets,
	}
	// When obmondo.monitoring is on, the `secrets` ArgoCD App (sync-order 10)
	// applies obmondo-clientcert as a SealedSecret in the monitoring namespace.
	// The monitoring namespace itself is created by kube-prometheus at
	// sync-order 50 — so without pre-creating it here, the secrets App deadlocks
	// and kube-prometheus never gets a chance to sync.
	if config.ParsedGeneralConfig.Obmondo != nil && config.ParsedGeneralConfig.Obmondo.Monitoring {
		namespacesToBeCreated = append(namespacesToBeCreated, "monitoring")
	}
	// On VPN clusters, kubeaid-cli pre-creates the namespaces so its
	// SealedSecret render lands ahead of ArgoCD's first sync of the
	// chart that consumes them:
	//   - cnpg-operator   : both modes (cnpg backs netbird-pgsql in
	//                       both, and keycloak-pgsql when managed).
	//   - netbird         : both modes (the netbird +
	//                       netbird-turn-credentials Secrets are
	//                       seeded for both).
	//   - keycloakx       : only when managed (keycloak-admin
	//                       SealedSecret consumed by the chart's
	//                       pre-install hook).
	if vpnClusterEnabled() {
		namespacesToBeCreated = append(namespacesToBeCreated,
			constants.NamespaceCloudNativePG,
			constants.NamespaceNetBird,
		)
	}
	if managedKeycloakEnabled() {
		namespacesToBeCreated = append(namespacesToBeCreated,
			constants.NamespaceKeycloak,
		)
	}
	for _, namespace := range namespacesToBeCreated {
		err := kubernetes.CreateNamespace(ctx, namespace, args.ClusterClient)
		assert.AssertErrNil(ctx, err, "Failed creating namespace",
			slog.String("namespace", namespace))
	}

	// Seed sealed-secrets controller key material BEFORE the chart
	// installs the controller. Two sources:
	//
	//   - DR recovery: restore key Secrets from the backup bucket.
	//   - Main-cluster bootstrap (non-DR): copy active key Secrets
	//     from the management cluster so the main controller can
	//     decrypt every SealedSecret kubeaid-cli sealed against the
	//     management controller during Phase 0.
	//
	// Doing this BEFORE Helm install means the controller's pod boots
	// with the existing keys already present — sealed-secrets-controller
	// uses them directly and skips generating a fresh "KEY-B", leaving
	// the cluster with one canonical key. Same shape as the prior DR
	// flow; the only addition is the management→main path.
	switch {
	case args.IsPartOfDisasterRecovery:
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
		err := kubernetes.ReplaceForceFromDir(ctx, args.ClusterClient, sealedSecretsKeysDirPath)
		assert.AssertErrNil(ctx, err, "Failed restoring sealed secrets private keys")

		slog.InfoContext(ctx,
			"Restored Sealed Secrets controller private keys",
			slog.String("dir-path", sealedSecretsKeysDirPath),
		)

	case args.ClusterType == constants.ClusterTypeMain:
		releaseCopy := bar.InProgress("Seeding sealed-secrets keys from management cluster")
		mgmtKubeconfigPath, mgmtErr := kubernetes.GetManagementClusterKubeconfigPath(ctx)
		assert.AssertErrNil(ctx, mgmtErr, "Failed getting management cluster kubeconfig path")

		mgmtClient, mgmtErr := kubernetes.CreateKubernetesClient(ctx, mgmtKubeconfigPath)
		assert.AssertErrNil(ctx, mgmtErr, "Failed constructing management cluster client")

		if err := kubernetes.CopySealedSecretsKeysFromManagement(ctx,
			mgmtClient, args.ClusterClient,
		); err != nil {
			releaseCopy()
			assert.AssertErrNil(ctx, err, "Failed seeding sealed-secrets keys")
		}
		releaseCopy()
		bar.Substep("Seeded sealed-secrets keys from management cluster")
	}

	// Install Sealed Secrets. Helm install brings up the controller,
	// which discovers any keys we pre-seeded above and uses them as-is.
	releaseSS := bar.InProgress("Installing Sealed Secrets controller")
	if err := kubernetes.InstallSealedSecrets(ctx); err != nil {
		releaseSS()
		assert.AssertErrNil(ctx, err, "Failed installing Sealed Secrets")
	}
	releaseSS()
	bar.Substep("Installed Sealed Secrets controller")

	// Verify the controller is actually serving — talk to the API
	// server, not Helm's release record. Two independent checks with
	// independent recovery actions:
	//   (1) every active sealed-secrets key Secret on mgmt is present
	//       on main; if not, re-run the copy.
	//   (2) the controller Deployment is fully Available; if not,
	//       run Install.Replace=true to force a fresh apply (covers
	//       the "Helm thinks deployed but Deployment was manually
	//       removed" case Helm can't otherwise detect).
	// One reinstall retry budget; rich diagnostic on final failure.
	//
	// Mgmt-cluster setup phase doesn't have a separate mgmt client to
	// compare against, so we use the same client on both sides — the
	// parity check trivially passes (same set, same count). The
	// Deployment-health check still earns its keep.
	mgmtClientForHealthCheck := args.ClusterClient
	if args.ClusterType == constants.ClusterTypeMain {
		mgmtKubeconfigPath, mgmtErr := kubernetes.GetManagementClusterKubeconfigPath(ctx)
		assert.AssertErrNil(ctx, mgmtErr, "Failed getting management cluster kubeconfig path")

		mgmtClient, mgmtErr := kubernetes.CreateKubernetesClient(ctx, mgmtKubeconfigPath)
		assert.AssertErrNil(ctx, mgmtErr, "Failed constructing management cluster client for health check")
		mgmtClientForHealthCheck = mgmtClient
	}

	releaseHealth := bar.InProgress("Verifying Sealed Secrets controller health")
	if err := kubernetes.EnsureSealedSecretsHealthy(ctx,
		mgmtClientForHealthCheck, args.ClusterClient,
	); err != nil {
		releaseHealth()
		assert.AssertErrNil(ctx, err, "Sealed Secrets controller not healthy")
	}
	releaseHealth()
	bar.Substep("Sealed Secrets controller healthy")

	SetupKubeAidConfig(ctx, SetupKubeAidConfigArgs{
		CreateDevEnvArgs: args.CreateDevEnvArgs,
		GitAuthMethod:    args.GitAuthMethod,
	})

	// Install and setup ArgoCD.
	releaseArgoCD := bar.InProgress("Installing and configuring ArgoCD")
	err := kubernetes.InstallAndSetupArgoCD(ctx, utils.GetClusterDir(), args.ClusterClient)
	releaseArgoCD()
	assert.AssertErrNil(ctx, err, "Failed installing and setting up ArgoCD")
	bar.Substep("Installed and configured ArgoCD")

	// Create the capi-cluster / capi-cluster-<customer-id> namespace, where the 'cloud-credentials'
	// Kubernetes Secret will get created.
	// Not needed for the local provider, since there is no CAPI cluster.
	if globals.CloudProviderName != constants.CloudProviderLocal {
		err := kubernetes.CreateNamespace(ctx, kubernetes.GetCapiClusterNamespace(), args.ClusterClient)
		assert.AssertErrNil(ctx, err, "Failed creating namespace",
			slog.String("namespace", kubernetes.GetCapiClusterNamespace()))
	}

	// Pre-create the netbird namespace when the netbird-operator is
	// rendered: the netbird-mgmt-api-key SealedSecret lands there via
	// the secrets app, which syncs BEFORE the netbird-operator app
	// would create its own namespace — without this, the secrets sync
	// fails on the missing namespace. Same rationale as capi-cluster
	// above.
	if netBirdOperatorEnabled() {
		err := kubernetes.CreateNamespace(ctx, constants.NamespaceNetBird, args.ClusterClient)
		assert.AssertErrNil(ctx, err, "Failed creating namespace",
			slog.String("namespace", constants.NamespaceNetBird))
	}

	// Same ordering for the DNS-01 issuer's Cloudflare token: its
	// SealedSecret targets the cert-manager namespace, which the
	// cert-manager app only creates AFTER the secrets app has synced.
	if acmeDNS01Enabled() {
		err := kubernetes.CreateNamespace(ctx, constants.NamespaceCertManager, args.ClusterClient)
		assert.AssertErrNil(ctx, err, "Failed creating namespace",
			slog.String("namespace", constants.NamespaceCertManager))
	}

	// Sync the Root, Secrets and CertManager ArgoCD Apps one by one.
	//
	// Order matters: root is the app-of-apps and just creates the child
	// Applications (no resource churn). secrets runs SECOND so every
	// downstream SealedSecret in kubeaid-config gets applied + decrypted
	// before its consumer chart syncs — cert-manager (Issuer Secrets),
	// netbird (envFromSecret), keycloakx (admin Secret), etc. all find
	// their backing Secret already in place when their app finally
	// syncs. Before this flip, cert-manager could race the secrets app
	// and end up in a brief CrashLoopBackOff waiting for its CA-key
	// Secret to materialise.
	//
	// On the MANAGEMENT cluster the root sync is narrowed with a
	// SyncOperationResource list so only the mgmt-relevant child Apps
	// get created — cilium / ccm-* / kube-prometheus / etc. are
	// workload-only and the mgmt cluster has no business deploying
	// them. Without the filter they'd show up in mgmt's ArgoCD UI as
	// Missing+OutOfSync ghosts forever. On the MAIN cluster we pass
	// no resource filter (nil) so root creates the full App set.
	argoCDAppsToBeSynced := []string{
		constants.ArgoCDAppRoot,
		"secrets",
		"cert-manager",
	}
	for _, argoCDApp := range argoCDAppsToBeSynced {
		var syncResources []*argoCDV1Alpha1.SyncOperationResource
		if argoCDApp == constants.ArgoCDAppRoot &&
			args.ClusterType == constants.ClusterTypeManagement {
			syncResources = managementClusterRootChildResources()
		}

		release := bar.InProgress(fmt.Sprintf("Syncing %s ArgoCD app", argoCDApp))
		err = kubernetes.SyncArgoCDApp(ctx, argoCDApp, syncResources)
		release()
		assert.AssertErrNil(ctx, err, "Failed syncing ArgoCD app",
			slog.String("app", argoCDApp))
		bar.Substep(fmt.Sprintf("Synced %s ArgoCD app", argoCDApp))
	}

	// Any cloud provider specific tasks.
	switch globals.CloudProviderName {
	case constants.CloudProviderAzure:
		// Install CrossPlane.
		// Then set it up, by installing required Providers, Functions, Compositions and
		// Composite Resource Definitions (XRDs).
		err = kubernetes.InstallAndSetupCrossplane(ctx)
		assert.AssertErrNil(ctx, err, "Failed installing and setting up Crossplane")

		// Doing the following once (i.e., while being in the management cluster) is enough.
		if args.ClusterType == constants.ClusterTypeManagement {
			cloudProviderAzure, castErr := azure.CloudProviderToAzure(globals.CloudProvider)
			assert.AssertErrNil(ctx, castErr, "Failed casting CloudProvider to Azure")

			// Create required infrastructure for Azure Workload Identity and Disaster Recovery,
			// using CrossPlane.
			provErr := cloudProviderAzure.ProvisionInfrastructure(ctx)
			assert.AssertErrNil(ctx, provErr, "Failed provisioning Azure infrastructure")

			// Create the OIDC provider.
			oidcErr := cloudProviderAzure.CreateOIDCProvider(ctx)
			assert.AssertErrNil(ctx, oidcErr, "Failed creating OIDC provider")

			// Retrieves details about the infrastructure provisioned using CrossPlane.
			detailsErr := cloudProviderAzure.GetInfrastructureDetails(ctx, args.ClusterClient)
			assert.AssertErrNil(ctx, detailsErr, "Failed retrieving infrastructure details")

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
		releaseCAPIOp := bar.InProgress("Syncing cluster-api-operator ArgoCD app")
		err = kubernetes.SyncArgoCDApp(ctx, "cluster-api-operator",
			[]*argoCDV1Alpha1.SyncOperationResource{},
		)
		releaseCAPIOp()
		assert.AssertErrNil(ctx, err, "Failed syncing cluster-api-operator ArgoCD app")
		bar.Substep("Synced cluster-api-operator ArgoCD app")

		//nolint:godox
		// Sync the Infrastructure Provider component of the capi-cluster ArgoCD App.
		// TODO : Use ArgoCD sync waves so that we don't need to explicitly sync the Infrastructure
		//        Provider component first.
		//
		// syncInfrastructureProvider emits its own ↻/✓ pair for each
		// of its two stages — the ArgoCD sync and the pod-running
		// wait — so the operator can tell at a glance whether time
		// is being spent in sync or in waiting for the controller
		// pod to come up. Don't add an outer wrapper here; that
		// would just cover both stages with one generic line.
		syncInfrastructureProvider(ctx, args.ClusterClient)
	}

	// Sync the capi-cluster ArgoCD app on the management cluster so
	// the Cluster + Machine + InfrastructureCluster resources land
	// before we print "Management cluster ready" — that box is the
	// "all done with management cluster" marker, so the substep
	// belongs above it. Skipped on the main cluster (post-pivot the
	// resources are already there) and on bare-metal (no CAPI).
	if args.ClusterType == constants.ClusterTypeManagement &&
		kubernetes.UsingClusterAPI() &&
		!kubernetes.IsClusterctlMoveExecuted(ctx) {
		releaseSync := bar.InProgress("Syncing capi-cluster ArgoCD app")
		err := kubernetes.SyncArgoCDApp(ctx, constants.ArgoCDAppCapiCluster,
			[]*argoCDV1Alpha1.SyncOperationResource{},
		)
		releaseSync()
		assert.AssertErrNil(ctx, err, "Failed syncing capi-cluster ArgoCD app")
		bar.Substep("Synced capi-cluster ArgoCD app")
	}

	printHelpTextForArgoCDDashboardAccess(ctx, args.ClusterType)
}

// Syncs the Infrastructure Provider component of the CAPI Cluster ArgoCD App and waits for the
// infrastructure specific CRDs to be installed and pod to be running.
func syncInfrastructureProvider(ctx context.Context, clusterClient client.Client) {
	bar := progress.FromCtx(ctx)
	providerName := globals.CloudProviderName

	// Sync the Infrastructure Provider component.
	//
	// SyncArgoCDApp blocks until ArgoCD reports the resource Synced —
	// which depends on the InfrastructureProvider CR going Ready. The
	// CAPI Operator can wedge that for 10+ minutes when something
	// downstream is wrong (missing namespace, RBAC denial, image pull
	// failure, manifestPatches reference a bad path). The spinner
	// shows the same generic "Syncing..." line the whole time and
	// the operator has no visible signal of WHAT's stuck. Launch a
	// background watcher that polls the InfrastructureProvider's
	// status conditions every 20s and surfaces any False/Unknown
	// condition via slog Warn — operator tailing the log sees the
	// actual blocker (e.g. "InstallFailed: namespaces capi-cluster
	// not found") within 20 seconds instead of debugging it cold.
	watchCtx, watchCancel := context.WithCancel(ctx)
	defer watchCancel()
	go logInfrastructureProviderConditions(watchCtx, clusterClient,
		getInfrastructureProviderName(), kubernetes.GetCapiClusterNamespace())

	releaseSync := bar.InProgress(
		fmt.Sprintf("Syncing %s infrastructure-provider component", providerName),
	)
	err := kubernetes.SyncArgoCDApp(ctx, constants.ArgoCDAppCapiCluster,
		[]*argoCDV1Alpha1.SyncOperationResource{
			{
				Group: "operator.cluster.x-k8s.io",
				Kind:  string(clusterctlV1.InfrastructureProviderType),
				Name:  getInfrastructureProviderName(),
			},
		},
	)
	releaseSync()
	watchCancel()
	assert.AssertErrNil(ctx, err, "Failed syncing capi-cluster infrastructure provider ArgoCD app")
	bar.Substep(
		fmt.Sprintf("Synced %s infrastructure-provider component", providerName),
	)

	capiClusterNamespace := kubernetes.GetCapiClusterNamespace()

	// Wait for the infrastructure specific CRDs to be installed and infrastructure provider component
	// pod to be running. This is the slow part — kubeaid-cli has
	// just told the cluster to install the CAPI Operator
	// InfrastructureProvider CR; the operator pulls the controller
	// image, creates the Deployment, scheduler runs the Pod, image
	// pull happens, container starts. On a cold node-pull this can
	// take several minutes. Surface it as its own ↻ substep so the
	// operator sees we're waiting on the cluster, not stuck on a
	// kubeaid-cli call.
	releaseWait := bar.InProgress(
		fmt.Sprintf("Waiting for %s controller pod to be Running in %s", providerName, capiClusterNamespace),
	)

	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("namespace", capiClusterNamespace),
	})

	// Wait for a Pod labelled `cluster.x-k8s.io/provider=
	// infrastructure-<providerName>` to reach Running. CAPI Operator
	// stamps this label on every Deployment it materialises from an
	// InfrastructureProvider CR — same convention across CAPA
	// (infrastructure-aws), CAPZ (infrastructure-azure), CAPH
	// (infrastructure-hetzner), and any future provider kubeaid-cli
	// supports. The label is the right gate: it identifies THE pod
	// we're waiting on without coupling to image names or
	// Deployment names which differ per provider.
	//
	// The previous check — "first pod listed in the namespace is
	// Running" — was fragile: a Pending provider pod at index 0
	// (alphabetic ordering puts caph- / capa- / capz- before capi-)
	// would keep the wait pinned even after other pods were happily
	// running, AND a stray Running pod from any earlier sync (capi-
	// controller-manager, the operator itself) would falsely satisfy
	// the wait before the provider was actually deployed.
	//
	// Iterate matching pods, return Running on the first hit. Also
	// drop the poll interval from 1m → 15s — minute-granular
	// polling on an interactive bootstrap means the operator can
	// stare at a stale spinner for nearly a full minute after the
	// pod actually came up.
	providerLabel := labels.SelectorFromSet(labels.Set{
		"cluster.x-k8s.io/provider": fmt.Sprintf("infrastructure-%s", providerName),
	})
	err = wait.PollUntilContextCancel(ctx, 15*time.Second, true,
		func(ctx context.Context) (bool, error) {
			podList := &coreV1.PodList{}
			if err := clusterClient.List(ctx, podList, &client.ListOptions{
				Namespace:     capiClusterNamespace,
				LabelSelector: providerLabel,
			}); err != nil {
				slog.WarnContext(ctx, "Listing infrastructure-provider pods failed; will retry",
					slog.Any("err", err),
				)
				return false, nil
			}
			for i := range podList.Items {
				if podList.Items[i].Status.Phase == coreV1.PodRunning {
					return true, nil
				}
			}
			slog.InfoContext(ctx,
				"Still waiting for the infrastructure provider controller pod",
				slog.Int("matching-pods", len(podList.Items)),
			)
			return false, nil
		},
	)
	releaseWait()
	assert.AssertErrNil(ctx, err,
		"Failed waiting for the infrastructure provider component to come up",
	)
	bar.Substep(
		fmt.Sprintf("%s controller pod Running", providerName),
	)
}

// logInfrastructureProviderConditions polls the InfrastructureProvider
// CR every 20 seconds and surfaces any False/Unknown condition via
// slog so an operator tailing the log sees the actual blocker (e.g.
// "InstallFailed: namespaces capi-cluster not found",
// "ImagePullBackOff", "EnsuringComponents") instead of staring at a
// generic "Syncing infrastructure-provider component" spinner.
//
// Returns when ctx is cancelled — the caller cancels it after
// SyncArgoCDApp returns. Failures to read the CR are logged at WARN
// but never abort: this is a diagnostic sidecar, not a critical path.
func logInfrastructureProviderConditions(
	ctx context.Context,
	clusterClient client.Client,
	name, namespace string,
) {
	gvk := schema.GroupVersionKind{
		Group:   "operator.cluster.x-k8s.io",
		Version: "v1alpha2",
		Kind:    string(clusterctlV1.InfrastructureProviderType),
	}

	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()

	logCtx := logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("provider", name),
		slog.String("namespace", namespace),
	})

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(gvk)
		err := clusterClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, obj)
		if err != nil {
			slog.WarnContext(logCtx, "Polling InfrastructureProvider failed; will retry",
				slog.Any("err", err),
			)
			continue
		}

		conditions, found, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
		if !found || len(conditions) == 0 {
			slog.InfoContext(logCtx,
				"InfrastructureProvider has no status conditions yet — operator hasn't observed it")
			continue
		}

		allReady := true
		for _, raw := range conditions {
			cond, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			ctype, _, _ := unstructured.NestedString(cond, "type")
			status, _, _ := unstructured.NestedString(cond, "status")
			if status == "True" {
				continue
			}
			allReady = false
			reason, _, _ := unstructured.NestedString(cond, "reason")
			message, _, _ := unstructured.NestedString(cond, "message")
			slog.WarnContext(logCtx, "InfrastructureProvider condition not ready",
				slog.String("type", ctype),
				slog.String("status", status),
				slog.String("reason", reason),
				slog.String("message", message),
			)
		}
		if allReady {
			slog.InfoContext(logCtx, "All InfrastructureProvider conditions are True; sync should complete shortly")
		}
	}
}

// getInfrastructureProviderName returns the name of the
// InfrastructureProvider CR rendered by the kubeaid capi-cluster
// chart. The chart hard-codes the name to the provider (e.g.
// "hetzner") with no customer-id suffix — see
// argocd-helm-charts/capi-cluster/templates/provider-{aws,azure,
// hetzner}.yaml (`$name := "hetzner"`). The earlier Obmondo-mode
// suffix here pointed kubeaid-cli at a CR that doesn't exist
// ("hetzner-enableit"), wedging ArgoCD's SyncOperationResource and
// the InfrastructureProvider condition watcher for the full sync
// window with `not found` errors. Matches the namespace-suffix
// drop in c4773bd / PR #563.
func getInfrastructureProviderName() string {
	return globals.CloudProviderName
}

// printHelpTextForArgoCDDashboardAccess renders the post-bootstrap
// "how to open the ArgoCD admin UI" steps as a lipgloss rounded-
// border box so it matches the visual language of the PR-merge
// prompt (renderPRMergeBox), the K8s version picker, and the
// DNS-wait table — all of which the operator has already seen as
// boxed surfaces during the same bootstrap. Cyan + underlined URL
// styling is auto-detected as a clickable link by iTerm2 / gnome-
// terminal / Alacritty / kitty so the operator can cmd-click
// instead of copy-pasting.
func printHelpTextForArgoCDDashboardAccess(ctx context.Context, clusterType string) {
	clusterKubeconfigPath := constants.OutputPathManagementClusterHostKubeconfig
	if clusterType == constants.ClusterTypeMain {
		clusterKubeconfigPath = constants.OutputPathMainClusterKubeconfig
	}

	// Header text per cluster type. Main cluster surfaces the configured
	// cluster name so the operator's terminal scrollback names the
	// thing they're looking at (especially helpful when they have
	// multiple kubeaid-cli runs against differently-named clusters
	// open in tmux panes side by side); management cluster stays
	// generic since it's the internal k3d node.
	var header string
	switch clusterType {
	case constants.ClusterTypeManagement:
		header = "✓ Management cluster ready"
	case constants.ClusterTypeMain:
		header = fmt.Sprintf("✓ %s k8s cluster is ready now.",
			config.ParsedGeneralConfig.Cluster.Name)
	default:
		header = "✓ " + clusterType + " cluster ready"
	}

	headerStyle := lipgloss.NewStyle().Bold(true)
	urlStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("39")). // bright blue — match renderPRMergeBox
		Underline(true)

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		headerStyle.Render(header),
		"",
		headerStyle.Render("ArgoCD admin dashboard"),
		"",
		" 1.  export KUBECONFIG="+clusterKubeconfigPath,
		"",
		" 2.  kubectl -n argocd get secret argocd-initial-admin-secret -o jsonpath='{.data.password}' | base64 -d",
		"",
		" 3.  kubectl -n argocd port-forward svc/argocd-server 8080:443",
		"",
		" 4.  Open "+urlStyle.Render("https://localhost:8080")+"  (user: admin)",
	)

	// Pause the progress bar so its 100ms spinner auto-render can't
	// \r-overwrite the box mid-print — without this the spinner line
	// tangles with the box's top border. Same fix as renderPRMergeBox.
	bar := progress.FromCtx(ctx)
	bar.Pause()
	defer bar.Resume()

	fmt.Println(lipgloss.NewStyle(). //nolint:forbidigo
						Border(lipgloss.RoundedBorder()).
						Padding(0, 1).
						Render(content))
}

// managementClusterRootChildResources returns the SyncOperationResource
// filter passed to the root ArgoCD App sync on the MANAGEMENT cluster.
// Root is an app-of-apps; without a filter it would create every child
// Application listed under argocd-apps/templates/ in kubeaid-config —
// including ones intended for the workload cluster (cilium, ccm-*,
// kube-prometheus, cluster-autoscaler, etc.). Those would sit
// Missing+OutOfSync on mgmt forever because mgmt has no business
// deploying them.
//
// On the management cluster only this minimal set is needed:
//   - argocd          — the ArgoCD chart reconciled (manages itself)
//   - sealed-secrets  — the controller chart reconciled
//   - secrets         — the SealedSecret manifests that decrypt to the
//     actual Secret resources kubeaid-cli pre-applied
//   - cert-manager    — issuers for ArgoCD's own TLS, kubeaid-cli's
//     management-cluster cert needs
//   - cluster-api-operator — the CAPI control-plane that provisions
//     the workload cluster
//   - capi-cluster    — the Cluster + Machine CRs the operator manages
//     pre-pivot (clusterctl move transfers them to
//     main afterwards)
//
// Each child is a Kind=Application in the argoproj.io group, deployed
// to the argocd namespace.
//
// On the MAIN cluster (post-pivot) we pass nil instead so root creates
// the full App set — the workload Apps belong there.
func managementClusterRootChildResources() []*argoCDV1Alpha1.SyncOperationResource {
	mgmtApps := []string{
		constants.ArgoCDAppArgoCD,
		constants.ArgoCDAppSealedSecrets,
		"secrets",
		"cert-manager",
		"cluster-api-operator",
		constants.ArgoCDAppCapiCluster,
	}
	resources := make([]*argoCDV1Alpha1.SyncOperationResource, 0, len(mgmtApps))
	for _, name := range mgmtApps {
		resources = append(resources, &argoCDV1Alpha1.SyncOperationResource{
			Group:     "argoproj.io",
			Kind:      "Application",
			Name:      name,
			Namespace: constants.NamespaceArgoCD,
		})
	}
	return resources
}
