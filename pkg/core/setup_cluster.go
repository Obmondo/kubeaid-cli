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
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	clusterctlV1 "sigs.k8s.io/cluster-api/cmd/clusterctl/api/v1alpha3"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud/azure"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	gitUtils "github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/git"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/kubernetes"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/progress"
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

	// When recovering a cluster, restore the Sealed Secrets controller private keys.
	if args.IsPartOfDisasterRecovery {
		// Create the sealed-secrets namespace.
		err := kubernetes.CreateNamespace(ctx, constants.NamespaceSealedSecrets, args.ClusterClient)
		assert.AssertErrNil(ctx, err, "Failed creating namespace",
			slog.String("namespace", constants.NamespaceSealedSecrets))

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
		err = kubernetes.ReplaceForceFromDir(ctx, args.ClusterClient, sealedSecretsKeysDirPath)
		assert.AssertErrNil(ctx, err, "Failed restoring sealed secrets private keys")

		slog.InfoContext(ctx,
			"Restored Sealed Secrets controller private keys",
			slog.String("dir-path", sealedSecretsKeysDirPath),
		)
	}

	// Install Sealed Secrets.
	releaseSS := bar.InProgress("Installing Sealed Secrets controller")
	if err := kubernetes.InstallSealedSecrets(ctx); err != nil {
		releaseSS()
		assert.AssertErrNil(ctx, err, "Failed installing Sealed Secrets")
	}
	releaseSS()
	bar.Substep("Installed Sealed Secrets controller")

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

	// Sync the Root, CertManager and Secrets ArgoCD Apps one by one.
	argoCDAppsToBeSynced := []string{
		constants.ArgoCDAppRoot,
		"cert-manager",
		"secrets",
	}
	for _, argoCDApp := range argoCDAppsToBeSynced {
		release := bar.InProgress(fmt.Sprintf("Syncing %s ArgoCD app", argoCDApp))
		err = kubernetes.SyncArgoCDApp(ctx, argoCDApp, []*argoCDV1Alpha1.SyncOperationResource{})
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

	printHelpTextForArgoCDDashboardAccess(args.ClusterType)
}

// Syncs the Infrastructure Provider component of the CAPI Cluster ArgoCD App and waits for the
// infrastructure specific CRDs to be installed and pod to be running.
func syncInfrastructureProvider(ctx context.Context, clusterClient client.Client) {
	bar := progress.FromCtx(ctx)
	providerName := globals.CloudProviderName

	// Sync the Infrastructure Provider component.
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

// Returns the name of the InfrastructureProvider component.
func getInfrastructureProviderName() string {
	infrastructureProviderName := globals.CloudProviderName

	if config.ParsedGeneralConfig.Obmondo != nil {
		infrastructureProviderName = infrastructureProviderName + "-" + config.ParsedGeneralConfig.Obmondo.CustomerID
	}

	return infrastructureProviderName
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
func printHelpTextForArgoCDDashboardAccess(clusterType string) {
	clusterKubeconfigPath := constants.OutputPathManagementClusterHostKubeconfig
	if clusterType == constants.ClusterTypeMain {
		clusterKubeconfigPath = constants.OutputPathMainClusterKubeconfig
	}

	// Title-case the cluster type for the box header. Two callers
	// only ever ("management" / "main"), so a switch is clearer than
	// pulling in the (deprecated) strings.Title.
	clusterTypeTitle := clusterType
	switch clusterType {
	case constants.ClusterTypeManagement:
		clusterTypeTitle = "Management"
	case constants.ClusterTypeMain:
		clusterTypeTitle = "Main"
	}

	headerStyle := lipgloss.NewStyle().Bold(true)
	urlStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("39")). // bright blue — match renderPRMergeBox
		Underline(true)

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		headerStyle.Render("✓ "+clusterTypeTitle+" cluster ready"),
		"",
		headerStyle.Render("ArgoCD admin dashboard"),
		"",
		" 1.  export KUBECONFIG="+clusterKubeconfigPath,
		"",
		" 2.  kubectl -n argocd get secret \\",
		"       argocd-initial-admin-secret \\",
		"       -o jsonpath='{.data.password}' | base64 -d",
		"",
		" 3.  kubectl -n argocd port-forward \\",
		"       svc/argocd-server 8080:443",
		"",
		" 4.  Open "+urlStyle.Render("https://localhost:8080")+"  (user: admin)",
	)

	fmt.Println(lipgloss.NewStyle(). //nolint:forbidigo
						Border(lipgloss.RoundedBorder()).
						Padding(0, 1).
						Render(content))
}
