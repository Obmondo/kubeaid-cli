// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package kubernetes

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path"
	"strings"
	"time"

	"github.com/argoproj/argo-cd/v2/pkg/apiclient"
	"github.com/argoproj/argo-cd/v2/pkg/apiclient/application"
	"github.com/argoproj/argo-cd/v2/pkg/apiclient/certificate"
	"github.com/argoproj/argo-cd/v2/pkg/apiclient/project"
	"github.com/argoproj/argo-cd/v2/pkg/apiclient/session"
	argoCDV1Aplha1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	"github.com/argoproj/argo-cd/v2/util/rbac"
	"github.com/argoproj/gitops-engine/pkg/health"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	valuesPkg "helm.sh/helm/v3/pkg/cli/values"
	coreV1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8sAPIErrors "k8s.io/apimachinery/pkg/api/errors"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	k8sRetry "k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/globals"
	"github.com/Obmondo/kubeaid-cli/pkg/utils"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/logger"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/progress"
)

type ArgoCDAppClient interface {
	List(ctx context.Context, q *application.ApplicationQuery, opts ...grpc.CallOption) (*argoCDV1Aplha1.ApplicationList, error)
	Sync(ctx context.Context, r *application.ApplicationSyncRequest, opts ...grpc.CallOption) (*argoCDV1Aplha1.Application, error)
	Get(ctx context.Context, q *application.ApplicationQuery, opts ...grpc.CallOption) (*argoCDV1Aplha1.Application, error)
}

var noResources []*argoCDV1Aplha1.SyncOperationResource

// syncArgoCDApp's re-issue loop intervals. Package-level vars so tests
// can shrink them; production keeps the original cadence.
var (
	// argoCDSyncInProgressBackoff is how long to wait when Sync reports
	// an operation is already in progress before re-checking.
	argoCDSyncInProgressBackoff = 10 * time.Second

	// argoCDSyncRetryInterval is how long to wait, after a Sync that
	// left the App still OutOfSync, before re-issuing Sync.
	argoCDSyncRetryInterval = 15 * time.Second

	// argoCDRepoFetchMaxAttempts / argoCDRepoFetchBackoff govern recovery
	// from transient git/SSH fetch failures during sync — slow SSH
	// handshake to a self-hosted gitea, repo-server pod briefly not
	// serving, etc. On each matching failure we hard-refresh the App so
	// argocd-application-controller re-asks repo-server, wait, then
	// retry Sync.
	argoCDRepoFetchMaxAttempts = 5
	argoCDRepoFetchBackoff     = 10 * time.Second

	// argoCDPortForwardMaxAttempts / argoCDPortForwardBackoff govern
	// recovery from the kubectl port-forward to argocd-server dying
	// mid-Sync. The classic trigger is syncing the argocd ArgoCD app
	// itself — that restarts argocd-server and severs the active
	// port-forward, so the next gRPC call comes back with the transport
	// code codes.Unavailable. On each such failure we reconnect
	// (re-port-forward + recreate the application client) and retry Sync.
	argoCDPortForwardMaxAttempts = 10
	argoCDPortForwardBackoff     = 5 * time.Second
)

type ArgoCDAppManager struct {
	client    ArgoCDAppClient
	reconnect func(ctx context.Context)
}

func NewArgoCDAppManager(appClient ArgoCDAppClient, reconnect func(ctx context.Context)) *ArgoCDAppManager {
	return &ArgoCDAppManager{
		client:    appClient,
		reconnect: reconnect,
	}
}

// Installs the ArgoCD Helm chart and creates the root ArgoCD App.
// Then creates and returns an ArgoCD Application client.
func InstallAndSetupArgoCD(ctx context.Context, clusterDir string, clusterClient client.Client) error {
	slog.InfoContext(ctx, "Installing and setting up ArgoCD")

	/*
	   Install the ArgoCD AppProject CRD.
	   Otherwise, we'll get error while installing the ArgoCD Helm chart, since it tries to create
	   the kubeaid ArgoCD App Project during installation.

	   NOTE : We need to retry, since raw.githubusercontent.com doesn't respond sometimes.
	*/
	for {
		err := ApplyManifestFromURL(ctx, clusterClient,
			"https://raw.githubusercontent.com/argoproj/argo-cd/refs/heads/master/manifests/crds/appproject-crd.yaml")
		if err == nil {
			break
		}
		time.Sleep(10 * time.Second)
	}

	if err := labelAppProjectCRDForHelm(ctx, clusterClient); err != nil {
		return fmt.Errorf("failed updating AppProject CRD labels/annotations: %w", err)
	}

	// Install the ArgoCD Helm chart. Pass the rendered values-argocd.yaml
	// from the kubeaid-config fork so the initial install gets the same
	// configs.ssh.knownHosts payload that the argocd Application will use
	// on self-sync — keeping argocd-ssh-known-hosts-cm populated before
	// the first root-app clone of a private git server.
	err := HelmInstall(ctx, &HelmInstallArgs{
		ChartPath: path.Join(utils.GetKubeAidDir(), "argocd-helm-charts/argo-cd"),

		Namespace:   constants.NamespaceArgoCD,
		ReleaseName: constants.ReleaseNameArgoCD,
		Values:      argoCDHelmValues(ctx, clusterDir),
	})
	if err != nil {
		return fmt.Errorf("failed installing ArgoCD Helm chart: %w", err)
	}

	// Port-forward ArgoCD and create ArgoCD client.
	argoCDClient, err := NewArgoCDClient(ctx, clusterClient)
	if err != nil {
		return fmt.Errorf("failed creating ArgoCD client: %w", err)
	}

	// Create the Kubernetes Secrets containing deploy keys,
	// which ArgoCD will use to access the KubeAid and KubeAid Config Git repositories.

	if config.ParsedGeneralConfig.Cluster.ArgoCD.DeployKeys.Kubeaid != nil {
		repoKubeaidSecretPath := path.Join(clusterDir, "sealed-secrets/argocd/repo-kubeaid.yaml")
		if err := ApplyManifestFromFile(ctx, clusterClient, repoKubeaidSecretPath); err != nil {
			return fmt.Errorf("failed applying repo-kubeaid secret: %w", err)
		}
	}

	repoKubeaidConfigSecretPath := path.Join(clusterDir, "sealed-secrets/argocd/repo-kubeaid-config.yaml")
	if err := ApplyManifestFromFile(ctx, clusterClient, repoKubeaidConfigSecretPath); err != nil {
		return fmt.Errorf("failed applying repo-kubeaid-config secret: %w", err)
	}

	// Add CA bundle for accessing customer's git server to ArgoCD.
	if len(config.ParsedGeneralConfig.Git.CABundle) > 0 {
		certClientCloser, certClient := argoCDClient.NewCertClientOrDie()
		defer certClientCloser.Close()

		_, err := certClient.CreateCertificate(ctx, &certificate.RepositoryCertificateCreateRequest{
			Upsert: true,
			Certificates: &argoCDV1Aplha1.RepositoryCertificateList{
				Items: []argoCDV1Aplha1.RepositoryCertificate{{
					ServerName: config.ParsedGeneralConfig.Forks.KubeaidConfigFork.ParsedURL.HostName(),
					CertType:   "https",
					CertData:   config.ParsedGeneralConfig.Git.CABundle,
				}},
			},
		})
		if err != nil {
			return fmt.Errorf(
				"failed adding CA bundle for accessing customer's git server to ArgoCD: %w", err,
			)
		}

		slog.InfoContext(ctx, "Added CA bundle (for accessing customer's git server) to ArgoCD")
	}

	// Create ArgoCD Application client.
	globals.ArgoCDApplicationClientCloser, globals.ArgoCDApplicationClient = argoCDClient.NewApplicationClientOrDie()

	// Create the root ArgoCD App.
	rootArgoCDAppPath := path.Join(clusterDir, "argocd-apps/templates/root.yaml")
	if err := ApplyManifestFromFile(ctx, clusterClient, rootArgoCDAppPath); err != nil {
		return fmt.Errorf("failed applying root ArgoCD app: %w", err)
	}
	slog.InfoContext(ctx, "Created root ArgoCD app")

	// When the user is an Obmondo customer, KubeAid Agent will get deployed to the cluster.
	// We need to setup the ArgoCD account created for KubeAid Agent.
	if (config.ParsedGeneralConfig.Obmondo != nil) &&
		(config.ParsedGeneralConfig.Obmondo.Monitoring) {

		projectClientCloser, projectClient := argoCDClient.NewProjectClientOrDie()
		defer projectClientCloser.Close()

		if err := setupKubeAgentArgoCDProjectRole(ctx, projectClient, clusterClient); err != nil {
			return fmt.Errorf("failed setting up KubeAid Agent ArgoCD project role: %w", err)
		}
	}

	return nil
}

// Port-forwards the ArgoCD server and creates an ArgoCD client.
// Returns the ArgoCD client.
func NewArgoCDClient(ctx context.Context, clusterClient client.Client) (apiclient.Client, error) {
	slog.InfoContext(ctx, "Creating ArgoCD client")

	// Create ArgoCD client (without auth token).
	argoCDClientOpts := &apiclient.ClientOptions{
		ServerName: "argocd-server",

		PortForward:          true,
		PortForwardNamespace: constants.NamespaceArgoCD,
		KubeOverrides: &clientcmd.ConfigOverrides{
			Timeout: "10s",
		},

		Insecure:     true,
		HttpRetryMax: 20,

		GRPCWeb: false,
	}
	argoCDClient := apiclient.NewClientOrDie(argoCDClientOpts)

	// Create a session using that ArgoCD client.
	argoCDClientSessionCloser, argoCDClientSession, err := argoCDClient.NewSessionClient()
	if err != nil {
		return nil, fmt.Errorf("failed creating session using ArgoCD client: %w", err)
	}
	defer argoCDClientSessionCloser.Close()

	// Retrieve ArgoCD admin password.
	argoCDAdminPassword, err := getArgoCDAdminPassword(ctx, clusterClient)
	if err != nil {
		return nil, fmt.Errorf("failed getting ArgoCD admin password: %w", err)
	}

	// Retrieve ArgoCD auth token.
	response, err := argoCDClientSession.Create(context.Background(), &session.SessionCreateRequest{
		Username: "admin",
		Password: argoCDAdminPassword,
	})
	if err != nil {
		return nil, fmt.Errorf("failed retrieving ArgoCD auth token: %w", err)
	}

	// Recreate ArgoCD client, with auth token.
	argoCDClientOpts.AuthToken = response.Token
	argoCDClient = apiclient.NewClientOrDie(argoCDClientOpts)

	return argoCDClient, nil
}

func labelAppProjectCRDForHelm(ctx context.Context, clusterClient client.Client) error {
	return k8sRetry.RetryOnConflict(k8sRetry.DefaultRetry, func() error {
		crd := &apiextensionsv1.CustomResourceDefinition{}
		if err := clusterClient.Get(ctx, types.NamespacedName{Name: "appprojects.argoproj.io"}, crd); err != nil {
			return fmt.Errorf("getting AppProject CRD: %w", err)
		}
		if crd.Labels == nil {
			crd.Labels = make(map[string]string)
		}
		crd.Labels["app.kubernetes.io/managed-by"] = "Helm"
		if crd.Annotations == nil {
			crd.Annotations = make(map[string]string)
		}
		crd.Annotations["meta.helm.sh/release-name"] = constants.ReleaseNameArgoCD
		crd.Annotations["meta.helm.sh/release-namespace"] = constants.NamespaceArgoCD

		return clusterClient.Update(ctx, crd)
	})
}

// CreateArgoCDProject creates an ArgoCD Project with the given name.
// Returns nil if the project already exists.
func CreateArgoCDProject(ctx context.Context, argoCDProjectClient project.ProjectServiceClient, name string) error {
	_, err := argoCDProjectClient.Create(ctx, &project.ProjectCreateRequest{
		Project: &argoCDV1Aplha1.AppProject{
			ObjectMeta: metaV1.ObjectMeta{
				Name: constants.ArgoCDProjectKubeAid,
			},
			Spec: argoCDV1Aplha1.AppProjectSpec{
				Description: "A list of Kubeaid ArgoCD applications",
				SourceRepos: []string{"*"},
				Destinations: []argoCDV1Aplha1.ApplicationDestination{{
					Server:    "*",
					Namespace: "*",
					Name:      "*",
				}},
				ClusterResourceWhitelist:   []metaV1.GroupKind{{Group: "*", Kind: "*"}},
				NamespaceResourceWhitelist: []metaV1.GroupKind{{Group: "*", Kind: "*"}},
			},
		},
	})
	if err != nil {
		gRPCResponseStatus, ok := status.FromError(err)
		if ok && (gRPCResponseStatus.Code() == codes.AlreadyExists) {
			slog.InfoContext(ctx,
				"Skipped creating kubeaid ArgoCD project, since it already exists",
			)
			return nil
		}

		return fmt.Errorf("failed creating kubeaid ArgoCD project: %w", err)
	}

	slog.InfoContext(ctx, "Created KubeAid ArgoCD project")
	return nil
}

// Recreates the ArgoCD Application client by port-forwarding the ArgoCD server.
// If the clusterClient is not provided (is nil), then it picks up the KUBECONFIG envionment
// variable and constructs the cluster client by itself.
func RecreateArgoCDApplicationClient(ctx context.Context, clusterClient client.Client) error {
	// Construct the cluster client, if not provided.
	if clusterClient == nil {
		kubeconfigPath := os.Getenv(constants.EnvNameKubeconfig)
		var err error
		clusterClient, err = CreateKubernetesClient(ctx, kubeconfigPath)
		if err != nil {
			return fmt.Errorf(
				"failed constructing Kubernetes cluster client (kubeconfig=%s): %w",
				kubeconfigPath,
				err,
			)
		}
	}

	// Port-forward ArgoCD and create ArgoCD client.
	argoCDClient, err := NewArgoCDClient(ctx, clusterClient)
	if err != nil {
		return fmt.Errorf("failed creating ArgoCD client: %w", err)
	}

	// Create ArgoCD Application client.
	globals.ArgoCDApplicationClientCloser, globals.ArgoCDApplicationClient = argoCDClient.NewApplicationClientOrDie()
	return nil
}

func newGlobalArgoCDAppManager() *ArgoCDAppManager {
	mgr := &ArgoCDAppManager{
		client: globals.ArgoCDApplicationClient,
	}
	mgr.reconnect = func(ctx context.Context) {
		if err := RecreateArgoCDApplicationClient(ctx, nil); err != nil {
			slog.ErrorContext(ctx, "Failed recreating ArgoCD application client during reconnect",
				logger.Error(err),
			)
			return
		}
		// Point the manager at the freshly-created application client.
		// Without this, every Sync/Get after a reconnect keeps hitting
		// the old (now-dead) port-forward — RecreateArgoCDApplicationClient
		// replaces the global, but mgr.client still holds the previous
		// interface value, so the retry loops above wouldn't make progress.
		mgr.client = globals.ArgoCDApplicationClient
	}
	return mgr
}

// AppSyncStep is one entry in SyncAllArgoCDApps's ordered list: an
// ArgoCD App to sync, plus an optional hook run immediately after it
// syncs (before the next step and before the remaining-apps loop).
//
// The bootstrap uses it to bring up the Hetzner VPN dependency chain in
// a guaranteed sequence — ccm → traefik → cert-manager → keycloakx →
// netbird — with the LB-DNS and TLS-cert gates wired in as AfterSync
// hooks, instead of relying on the alphabetical order ArgoCD's List
// happens to return.
type AppSyncStep struct {
	// Name is the ArgoCD App name. A step whose App isn't present in
	// the cluster is skipped.
	Name string

	// AfterSync, if non-nil, runs once Name has synced — before the
	// next step. The bootstrap gates here on, e.g., the App's
	// cert-manager Certificate being Ready: a Synced ArgoCD App only
	// means its manifests (Ingress included) were applied, not that
	// the TLS cert was actually issued.
	AfterSync func(context.Context) error
}

// SyncAllArgoCDApps lists and syncs all the ArgoCD Apps.
//
// orderedApps are synced first, in slice order, each immediately
// followed by its AfterSync hook (if any). Every other App is then
// synced in a generic loop. A step whose App isn't present in the
// cluster is skipped.
func SyncAllArgoCDApps(ctx context.Context,
	skipMonitoringSetup bool,
	orderedApps []AppSyncStep,
) error {
	mgr := newGlobalArgoCDAppManager()
	return mgr.syncAllArgoCDApps(ctx, skipMonitoringSetup, orderedApps)
}

// WaitForArgoCDAppHealthy blocks until the named ArgoCD App
// reports both Sync=Synced and Health=Healthy. Used by callers
// that need to do follow-on work against the underlying
// application (e.g. talk to Keycloak admin API once the
// keycloakx App is fully up).
func WaitForArgoCDAppHealthy(ctx context.Context, name string) error {
	mgr := newGlobalArgoCDAppManager()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if mgr.isArgoCDAppHealthy(ctx, name) {
			return nil
		}
		slog.InfoContext(ctx, "Waiting for ArgoCD App to be Healthy",
			slog.String("app", name),
		)
		time.Sleep(15 * time.Second)
	}
}

// isArgoCDAppHealthy returns true when the named ArgoCD App is
// both Synced and Healthy. Re-uses the reconnect-on-error retry
// loop from isArgoCDAppSynced so transient API server unavailable
// during ArgoCD restarts don't surface as a hard failure here.
func (m *ArgoCDAppManager) isArgoCDAppHealthy(ctx context.Context, name string) bool {
	var (
		argoCDApp *argoCDV1Aplha1.Application
		err       error
	)
	for {
		argoCDApp, err = m.client.Get(ctx, &application.ApplicationQuery{
			Name:         &name,
			Project:      []string{constants.ArgoCDProjectKubeAid},
			AppNamespace: ptr.To(constants.NamespaceArgoCD),
			Refresh:      ptr.To(string(argoCDV1Aplha1.RefreshTypeNormal)),
		})
		if err == nil {
			break
		}

		slog.ErrorContext(ctx,
			"Failed getting ArgoCD App. Retrying after 10 seconds....",
			logger.Error(err),
		)
		time.Sleep(10 * time.Second)

		if m.reconnect != nil {
			m.reconnect(ctx)
		}
	}

	return argoCDApp.Status.Sync.Status == argoCDV1Aplha1.SyncStatusCodeSynced &&
		argoCDApp.Status.Health.Status == health.HealthStatusHealthy
}

// syncAllArgoCDApps is the testable implementation of SyncAllArgoCDApps.
func (m *ArgoCDAppManager) syncAllArgoCDApps(ctx context.Context,
	skipMonitoringSetup bool,
	orderedApps []AppSyncStep,
) error {
	slog.InfoContext(ctx, "Syncing all ArgoCD Apps....")

	// Sync the root ArgoCD App first, so any uncreated ArgoCD Apps get created.
	if err := m.syncArgoCDAppWithProgress(ctx, constants.ArgoCDAppRoot, noResources); err != nil {
		return err
	}

	// Sync the sealed-secrets ArgoCD App next. The sealed-secrets
	// controller is installed directly via Helm during the management-
	// cluster setup phase (before ArgoCD exists), so its Service,
	// ServiceAccount, Deployment etc. live in the cluster without
	// ArgoCD's argocd.argoproj.io/tracking-id annotation — every
	// downstream view shows the sealed-secrets App as OutOfSync until
	// ArgoCD reconciles those resources and claims them. Syncing here
	// transfers ownership cleanly while bootstrap is still in flight,
	// so the App ends up Synced + Healthy with no lingering diff.
	if err := m.syncArgoCDAppWithProgress(ctx, constants.ArgoCDAppSealedSecrets, noResources); err != nil {
		return err
	}

	// Sync the CSI-driver ArgoCD App(s) for this cloud provider so
	// StorageClasses exist before stateful workloads sync.
	if err := m.syncCSIDriverApps(ctx); err != nil {
		return err
	}

	// Sync the KubePrometheus ArgoCD App, if monitoring setup is enabled.
	// Some ArgoCD Apps depend on the CRDs coming from the KubePrometheus ArgoCD App.
	if !skipMonitoringSetup {
		if err := m.syncArgoCDAppWithProgress(ctx, constants.ArgoCDAppKubePrometheus, noResources); err != nil {
			return err
		}
	}

	// List the ArgoCD Apps. The explicitly-ordered apps synced above
	// (root, sealed-secrets, CSI, kube-prometheus) are in here too;
	// they're re-visited in the final loop but skipped cheaply, since
	// syncArgoCDApp short-circuits an already-Synced App.
	response, err := m.client.List(ctx, &application.ApplicationQuery{})
	if err != nil {
		return fmt.Errorf("failed listing ArgoCD apps: %w", err)
	}

	// Sync the caller's ordered apps next — in slice order, each
	// immediately followed by its AfterSync hook. This gives the
	// bootstrap a guaranteed sequence (ccm → traefik → cert-manager →
	// keycloakx → netbird, with the LB-DNS and TLS-cert gates wired in
	// as hooks) rather than relying on the alphabetical order ArgoCD's
	// List returns. A step whose App isn't present is skipped.
	syncedAsStep := make(map[string]bool, len(orderedApps))
	for _, step := range orderedApps {
		if !argoCDAppListContains(response.Items, step.Name) {
			continue
		}
		if err := m.syncArgoCDAppWithProgress(ctx, step.Name, noResources); err != nil {
			return err
		}
		syncedAsStep[step.Name] = true

		if step.AfterSync != nil {
			if err := step.AfterSync(ctx); err != nil {
				return fmt.Errorf("after-sync hook for ArgoCD app %q failed: %w", step.Name, err)
			}
		}
	}

	// Sync each of the remaining ArgoCD Apps — those not already synced
	// as an ordered step above.
	for _, item := range response.Items {
		if syncedAsStep[item.Name] {
			continue
		}
		if err := m.syncArgoCDAppWithProgress(ctx, item.Name, noResources); err != nil {
			return err
		}
	}

	return nil
}

// syncCSIDriverApps syncs the CSI-driver ArgoCD App(s) for the current
// cloud provider, so StorageClasses exist before stateful workloads
// sync. No-op for AWS (the EBS CSI App + values templates aren't wired
// up yet).
func (m *ArgoCDAppManager) syncCSIDriverApps(ctx context.Context) error {
	switch globals.CloudProviderName {
	case constants.CloudProviderAWS:
		// TODO : Sync the AWS EBS CSI Driver ArgoCD App.
		//        We need to add the corresponding ArgoCD App and values file templates first.

	case constants.CloudProviderAzure:
		if err := m.syncArgoCDAppWithProgress(ctx, constants.ArgoCDAppAzureDiskCSIDriver, noResources); err != nil {
			return err
		}

	case constants.CloudProviderHetzner:
		if config.UsingHCloud() {
			if err := m.syncArgoCDAppWithProgress(ctx, constants.ArgoCDAppHCloudCSIDriver, noResources); err != nil {
				return err
			}
		}

		if config.UsingHetznerBareMetal() {
			// TODO : Sync the OpenEBS ZFS LocalPV ArgoCD App.

			if err := m.syncArgoCDAppWithProgress(ctx, constants.ArgoCDAppRookCeph, noResources); err != nil {
				return err
			}
		}

	case constants.CloudProviderBareMetal:
		if err := m.syncArgoCDAppWithProgress(ctx, constants.ArgoCDAppLocalPVProvisioner, noResources); err != nil {
			return err
		}
	}

	return nil
}

// argoCDAppListContains reports whether apps contains an Application
// named name.
func argoCDAppListContains(apps []argoCDV1Aplha1.Application, name string) bool {
	for i := range apps {
		if apps[i].Name == name {
			return true
		}
	}
	return false
}

// isArgoCDRepoFetchError reports whether err from m.client.Sync looks
// like ArgoCD's repo-server failed to fetch the source git repo —
// typically a slow SSH handshake to a self-hosted gitea, transient TCP
// hiccup, or repo-server briefly not serving. A hard refresh on the
// App prods argocd-application-controller to re-fetch via repo-server,
// which usually succeeds on a retry once the underlying transport
// recovers.
//
// The gRPC code is the typed signal — ArgoCD returns FailedPrecondition
// for repo-revision-resolution failures. The substring guard scopes us
// inside that bucket (other FailedPrecondition causes like "spec
// prevents sync" or "app being deleted" wouldn't recover from a hard
// refresh and shouldn't loop here). The underlying ssh.handshake Go
// error is lost across the gRPC boundary into the apiserver, so the
// SSH-specific nature only survives in the status description.
func isArgoCDRepoFetchError(err error) bool {
	if status.Code(err) != codes.FailedPrecondition {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "resolving repo revision") ||
		strings.Contains(msg, "failed to list refs") ||
		strings.Contains(msg, "ssh: handshake failed")
}

// isArgoCDTransientTransportError reports whether err from m.client.Sync
// is a transient transport-layer failure between kubeaid-cli and the
// argocd-server — almost always the kubectl port-forward dying mid-call.
// The headline trigger is syncing the argocd ArgoCD app itself: that
// restarts argocd-server and severs the active port-forward.
//
// gRPC surfaces that one underlying condition through several
// descriptions depending on timing — "connect: connection refused" and
// "error dial proxy" (a re-dial failed), "keepalive ping failed to
// receive ACK within timeout" (an established connection went dead),
// "transport is closing", "broken pipe". They all carry
// codes.Unavailable, gRPC's canonical retryable code: "the service is
// currently unavailable ... most likely a transient condition, which
// can be corrected by retrying with a backoff". So we classify on the
// code alone. An earlier substring allowlist over the description only
// ever produced false negatives — a new wording (the keepalive timeout)
// slipped straight through to a fatal return. A genuinely-down
// argocd-server still terminates the bootstrap: the caller bounds
// retries at argoCDPortForwardMaxAttempts.
func isArgoCDTransientTransportError(err error) bool {
	return status.Code(err) == codes.Unavailable
}

// syncArgoCDAppWithProgress wraps syncArgoCDApp with the
// "↻ Syncing X" / "✓ Synced X" sub-step pair so the operator sees
// which app is in flight at any given moment. Used by
// syncAllArgoCDApps where the loop visits a dozen+ apps in
// sequence — without per-app progress markers the spinner sits
// silent for minutes between log lines.
func (m *ArgoCDAppManager) syncArgoCDAppWithProgress(
	ctx context.Context,
	name string,
	resources []*argoCDV1Aplha1.SyncOperationResource,
) error {
	bar := progress.FromCtx(ctx)
	release := bar.InProgress(fmt.Sprintf("Syncing %s ArgoCD app", name))
	if err := m.syncArgoCDApp(ctx, name, resources); err != nil {
		release()
		return err
	}
	release()
	bar.Substep(fmt.Sprintf("Synced %s ArgoCD app", name))
	return nil
}

func SyncArgoCDApp(ctx context.Context, name string, resources []*argoCDV1Aplha1.SyncOperationResource) error {
	mgr := newGlobalArgoCDAppManager()
	return mgr.syncArgoCDApp(ctx, name, resources)
}

// syncArgoCDApp is the testable implementation of SyncArgoCDApp.
func (m *ArgoCDAppManager) syncArgoCDApp(ctx context.Context, name string, resources []*argoCDV1Aplha1.SyncOperationResource) error {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("app-name", name),
	})

	// Skip, if the ArgoCD App is already synced.
	if m.isArgoCDAppSynced(ctx, name, resources) {
		slog.InfoContext(ctx, "Skipped syncing ArgoCD application")
		return nil
	}

	// Sync the ArgoCD app.
	slog.InfoContext(ctx, "Syncing ArgoCD application")

	appNamespace := constants.NamespaceArgoCD
	// Per-request SyncOptions intentionally omitted — Argo CD treats a
	// non-nil SyncOptions on the request as REPLACING the App's
	// spec.syncPolicy.syncOptions, not merging. Earlier shape
	// hard-coded `[CreateNamespace=true, ApplyOutOfSyncOnly=true]` here
	// and only appended `ServerSideApply=true` for kube-prometheus +
	// rook-ceph. That silently stripped `ServerSideApply=true` from
	// every other App's declared syncPolicy when kubeaid-cli triggered
	// sync — cloudnative-pg's Cluster/Pooler CRDs (256 KiB+ schemas)
	// hit the `metadata.annotations` limit because Argo fell back to
	// client-side apply with its huge last-applied-configuration
	// annotation. Manual sync from the Argo UI worked because the UI
	// doesn't override syncOptions → Argo respected the App spec.
	//
	// Now: trust the App's spec.syncPolicy.syncOptions as the single
	// source of truth. Each Application template declares what it
	// needs (CreateNamespace, ApplyOutOfSyncOnly, ServerSideApply,
	// etc.). kubeaid-cli's job is to TRIGGER the sync, not to override
	// how it's applied.
	applicationSyncRequest := &application.ApplicationSyncRequest{
		Name:         &name,
		AppNamespace: &appNamespace,
		RetryStrategy: &argoCDV1Aplha1.RetryStrategy{
			Limit: 3,
			Backoff: &argoCDV1Aplha1.Backoff{
				Duration: "10s",
			},
		},
	}
	if len(resources) > 0 {
		applicationSyncRequest.Resources = resources
	}

	if name == constants.ArgoCDAppRookCeph {
		slog.WarnContext(ctx, `
It takes a very good amount of time to sync the Rook CEPH ArgoCD App initially. So, be
patient!

And we suggest, you take a look at the Rook CEPH pods yourself, via K9s. When getting
deployed, the monitoring pods might land up on the wrong node and be stuck in Pending state.
For now, please restart them manually. Later, we'll make KubeAid CLI do it.
    `)
	}

	attempts := syncRetryAttempts{}
	for {
		_, err := m.client.Sync(ctx, applicationSyncRequest)
		if err != nil {
			retry, fatalErr := m.classifyAndHandleSyncError(ctx, err, name, appNamespace, &attempts)
			if fatalErr != nil {
				return fatalErr
			}
			if retry {
				continue
			}
		}

		// Wait for ArgoCD to materialize the root app's child Applications,
		// and for argocd-repo-server to start serving on :8081 — otherwise
		// the subsequent per-child sync hits "connection refused" or "App
		// not found" on revision resolution.
		//
		// `resources` is the per-request sync-scope filter the caller
		// passed. On the management cluster it narrows the root sync to
		// the mgmt-app subset (see managementClusterRootChildResources);
		// on the main cluster post-pivot it's nil so root creates the
		// full App set. Passing it through means the wait expects
		// exactly the Application CRs that the sync ACTUALLY asked to
		// create — not every kind:Application declared under root, which
		// on mgmt always includes 7 workload-only Apps (cilium,
		// ccm-hetzner, kube-prometheus, ...) that the filter
		// deliberately excluded.
		if name == constants.ArgoCDAppRoot {
			return waitForRootArgoCDChildren(ctx, syncResourceAppNames(resources))
		}

		// isArgoCDAppSynced hard-refreshes the App, so this both checks the
		// result and lets the just-triggered operation make progress.
		if m.isArgoCDAppSynced(ctx, name, resources) {
			return nil
		}

		// Not synced yet — loop back and re-issue Sync. The operation may
		// still be running (the next Sync then returns "another operation
		// is already in progress", handled above), or it may have finished
		// and left the App OutOfSync — e.g. the sync hit a manifest-
		// generation error that has since been fixed upstream, or a
		// resource needs another apply pass — in which case a fresh
		// operation kicks off. The previous shape only re-polled with a
		// hard refresh and never re-issued Sync, so a single failed
		// operation wedged the bootstrap here indefinitely.
		slog.InfoContext(ctx, "ArgoCD App not synced yet; re-triggering sync")
		time.Sleep(argoCDSyncRetryInterval)
	}
}

// syncRetryAttempts tracks the bounded retry counters per error class
// that syncArgoCDApp's loop is allowed to burn through. Held by-pointer
// in classifyAndHandleSyncError so the counters survive across loop
// iterations.
type syncRetryAttempts struct {
	repoFetch   int
	portForward int
}

// classifyAndHandleSyncError inspects err from m.client.Sync and decides
// whether the loop should retry, return a wrapped fatal error, or fall
// through to the post-success path.
//
// Returns:
//   - retry=true,  fatalErr=nil — caller should `continue` the loop.
//     A backoff sleep / reconnect / hard-refresh has already happened
//     here as appropriate for the matched error class.
//   - retry=false, fatalErr!=nil — caller should `return fatalErr`.
//   - retry=false, fatalErr=nil — caller should fall through to the
//     post-success path (currently unreachable; kept so the call site
//     stays defensive).
func (m *ArgoCDAppManager) classifyAndHandleSyncError(
	ctx context.Context,
	err error,
	name, appNamespace string,
	attempts *syncRetryAttempts,
) (retry bool, fatalErr error) {
	// A sync operation triggered by a previous iteration (or by
	// ArgoCD's own auto-sync) is still running. Wait for it to
	// finish, then loop back and re-evaluate.
	if strings.Contains(err.Error(), "another operation is already in progress") {
		slog.WarnContext(ctx,
			"An ArgoCD App sync operation is already in progress. Waiting before retrying",
			logger.Error(err),
		)
		time.Sleep(argoCDSyncInProgressBackoff)
		return true, nil
	}

	// Port-forward died mid-Sync — typically because syncing the
	// argocd app itself restarted argocd-server. Re-establish the
	// port-forward (which also refreshes m.client), wait, then retry
	// Sync. Bounded so a genuinely-down apiserver fails out instead
	// of spinning forever.
	if isArgoCDTransientTransportError(err) && attempts.portForward < argoCDPortForwardMaxAttempts {
		attempts.portForward++
		slog.WarnContext(ctx,
			"ArgoCD sync failed on transport (port-forward likely died); reconnecting and retrying",
			slog.Int("attempt", attempts.portForward),
			slog.Int("max-attempts", argoCDPortForwardMaxAttempts),
			logger.Error(err),
		)
		if m.reconnect != nil {
			m.reconnect(ctx)
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(argoCDPortForwardBackoff):
		}
		return true, nil
	}

	// Repo-fetch failure (slow SSH handshake to gitea, repo-server
	// hiccup, etc.) — issue a hard refresh so ArgoCD re-asks
	// repo-server for the source revision, then retry Sync. Bounded
	// so a genuinely-broken repo URL fails out instead of looping
	// forever.
	if isArgoCDRepoFetchError(err) && attempts.repoFetch < argoCDRepoFetchMaxAttempts {
		attempts.repoFetch++
		slog.WarnContext(ctx,
			"ArgoCD sync failed on repo fetch; hard-refreshing and retrying",
			slog.Int("attempt", attempts.repoFetch),
			slog.Int("max-attempts", argoCDRepoFetchMaxAttempts),
			logger.Error(err),
		)
		if _, refreshErr := m.client.Get(ctx, &application.ApplicationQuery{
			Name:         &name,
			Project:      []string{constants.ArgoCDProjectKubeAid},
			AppNamespace: &appNamespace,
			Refresh:      ptr.To(string(argoCDV1Aplha1.RefreshTypeHard)),
		}); refreshErr != nil {
			slog.WarnContext(ctx,
				"Hard refresh request failed; will retry sync anyway",
				logger.Error(refreshErr),
			)
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(argoCDRepoFetchBackoff):
		}
		return true, nil
	}

	return false, fmt.Errorf("failed syncing ArgoCD application %q: %w", name, err)
}

// waitForRootArgoCDChildren blocks until the root ArgoCD app has been
// reconciled and the expected child Applications exist as Application
// CRs.
//
// "Reconciled" means root.status.sync.status is Synced or OutOfSync —
// either way, ArgoCD has successfully fetched the source repo from
// repo-server. Out-of-diff is fine; we just need proof that the repo is
// reachable so the subsequent per-child syncs don't trip "connection
// refused" on :8081 during revision resolution.
//
// `expected` is the list of child Application names we actually asked
// the sync operation to create:
//
//   - Non-empty (management cluster, narrowed sync) → wait for exactly
//     these. root.status.resources also lists the workload-only
//     children declared in the source repo (cilium, ccm-hetzner, etc.);
//     waiting on those would loop until the 3-minute deadline because
//     the sync was scoped to exclude them by design.
//   - Empty (main cluster, full root sync with resources == nil) →
//     fall back to every kind:Application listed in root.status.resources,
//     so the wait stays in sync with whatever the source repo declares.
//
// Errors on context cancellation or 3-minute deadline.
func waitForRootArgoCDChildren(ctx context.Context, expected []string) error {
	deadline := time.Now().Add(3 * time.Minute)
	for {
		reconciled, declared, missing, err := rootAppReadyForChildSync(ctx, expected)
		if err != nil {
			slog.WarnContext(ctx,
				"Failed querying root ArgoCD app",
				logger.Error(err),
			)
		}

		// declared > 0 guards against returning "ready" before ArgoCD has
		// populated root.status.resources at least once — immediately after
		// creation it's empty.
		if err == nil && reconciled && declared > 0 && len(missing) == 0 {
			slog.InfoContext(ctx,
				"Root app reconciled and all child apps materialized",
				slog.Int("child-apps", declared),
			)
			return nil
		}

		if time.Now().After(deadline) {
			return fmt.Errorf(
				"timed out waiting for root ArgoCD app (reconciled=%t, declared=%d, missing=%v)",
				reconciled, declared, missing,
			)
		}

		slog.InfoContext(ctx,
			"Waiting for root ArgoCD app to reconcile and child apps to materialize",
			slog.Bool("root-reconciled", reconciled),
			slog.Int("declared-children", declared),
			slog.Any("missing", missing),
		)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}

// rootAppReadyForChildSync returns the bootstrap-relevant state of the
// root ArgoCD app:
//
//   - reconciled: root.status.sync.status is Synced or OutOfSync — i.e.
//     ArgoCD has talked to repo-server and computed a diff at least once.
//     Out-of-sync is fine; we only need proof the repo is reachable.
//   - declared:   number of child Application CRs the caller cares about
//     (see `expected` below). 0 means ArgoCD hasn't yet listed root's
//     children — keep waiting.
//   - missing:    expected child names that don't yet exist as Application CRs.
//   - err:        transient API error; caller should keep retrying.
//
// `expected` selects which child Applications to watch for. Pass the
// names that the just-issued sync operation was scoped to (the
// SyncOperationResource.Name list). When empty/nil, falls back to every
// kind:Application listed under root.status.resources — appropriate
// for an un-narrowed sync on the main cluster.
func rootAppReadyForChildSync(ctx context.Context, expected []string) (bool, int, []string, error) {
	rootName := constants.ArgoCDAppRoot
	rootApp, err := globals.ArgoCDApplicationClient.Get(ctx, &application.ApplicationQuery{
		Name:         &rootName,
		Project:      []string{constants.ArgoCDProjectKubeAid},
		AppNamespace: ptr.To(constants.NamespaceArgoCD),
	})
	if err != nil {
		if reconnectErr := RecreateArgoCDApplicationClient(ctx, nil); reconnectErr != nil {
			slog.WarnContext(ctx, "Failed reconnecting ArgoCD application client",
				logger.Error(reconnectErr),
			)
		}
		return false, 0, nil, fmt.Errorf("getting root ArgoCD app: %w", err)
	}

	reconciled := rootApp.Status.Sync.Status == argoCDV1Aplha1.SyncStatusCodeSynced ||
		rootApp.Status.Sync.Status == argoCDV1Aplha1.SyncStatusCodeOutOfSync

	// Pick the watch-set: the caller's filter wins, since root.status.resources
	// over-declares relative to what was actually applied on the management
	// cluster (the workload-only Apps live there even though the sync was
	// narrowed to exclude them).
	watchSet := expected
	if len(watchSet) == 0 {
		for _, r := range rootApp.Status.Resources {
			if r.Kind == "Application" {
				watchSet = append(watchSet, r.Name)
			}
		}
	}
	if len(watchSet) == 0 {
		return reconciled, 0, nil, nil
	}

	list, err := globals.ArgoCDApplicationClient.List(ctx, &application.ApplicationQuery{
		AppNamespace: ptr.To(constants.NamespaceArgoCD),
	})
	if err != nil {
		if reconnectErr := RecreateArgoCDApplicationClient(ctx, nil); reconnectErr != nil {
			slog.WarnContext(ctx, "Failed reconnecting ArgoCD application client",
				logger.Error(reconnectErr),
			)
		}
		return reconciled, len(watchSet), nil, fmt.Errorf("listing ArgoCD apps: %w", err)
	}

	existing := map[string]bool{}
	for _, app := range list.Items {
		existing[app.Name] = true
	}

	var missing []string
	for _, name := range watchSet {
		if !existing[name] {
			missing = append(missing, name)
		}
	}
	return reconciled, len(watchSet), missing, nil
}

// syncResourceAppNames returns the .Name of every kind:Application in
// resources, in input order. Returns nil when resources is empty so
// callers can treat "no filter" as "watch everything root declares" —
// see waitForRootArgoCDChildren for the contract.
func syncResourceAppNames(resources []*argoCDV1Aplha1.SyncOperationResource) []string {
	if len(resources) == 0 {
		return nil
	}
	out := make([]string, 0, len(resources))
	for _, r := range resources {
		if r != nil && r.Kind == "Application" {
			out = append(out, r.Name)
		}
	}
	return out
}

// isArgoCDAppSynced returns whether the given ArgoCD App is synced or not.
// If the resources array is empty, checks whether the whole ArgoCD App is synced. Otherwise,
// only checks for the specified resources.
func (m *ArgoCDAppManager) isArgoCDAppSynced(ctx context.Context, name string, resources []*argoCDV1Aplha1.SyncOperationResource) bool {
	var (
		argoCDApp *argoCDV1Aplha1.Application
		err       error
	)
	// We need a retrial mechanism, because when we sync the argocd ArgoCD App, the ArgoCD pod may
	// get restarted, which will cause a failure. Then, we need to again port-forward the ArgoCD
	// server and completely reconstruct the ArgoCD Application client.
	for {
		// Get the ArgoCD App.
		argoCDApp, err = m.client.Get(ctx, &application.ApplicationQuery{
			Name:         &name,
			Project:      []string{constants.ArgoCDProjectKubeAid},
			AppNamespace: ptr.To(constants.NamespaceArgoCD),
			Refresh:      ptr.To(string(argoCDV1Aplha1.RefreshTypeHard)),
		})
		if err == nil {
			break
		}

		slog.ErrorContext(ctx,
			"Failed getting ArgoCD App. Retrying after 10 seconds....",
			logger.Error(err),
		)
		time.Sleep(10 * time.Second)

		// Port-forward the ArgoCD server pod and recreate the ArgoCD Application client.
		if m.reconnect != nil {
			m.reconnect(ctx)
		}
	}

	switch {
	// Only check that the specified resources are synced.
	case len(resources) > 0:
		{
			syncedResourcesMap := make(map[string]bool)
			for _, resource := range argoCDApp.Status.Resources {
				key := fmt.Sprintf("%s/%s/%s", resource.Group, resource.Kind, resource.Name)
				syncedResourcesMap[key] = (resource.Status == argoCDV1Aplha1.SyncStatusCodeSynced)
			}

			for _, resource := range resources {
				key := fmt.Sprintf("%s/%s/%s", resource.Group, resource.Kind, resource.Name)
				synced, exists := syncedResourcesMap[key]
				if !exists || !synced {
					return false
				}
			}
			return true
		}

	// In case of Velero ArgoCD App, check that all the resources (except Schedules and Backups)
	// are synced.
	case name == constants.ArgoCDAppVelero:
		for _, resource := range argoCDApp.Status.Resources {
			if resource.Kind == "Schedule" || resource.Kind == "Backup" {
				continue
			}

			if resource.Status != argoCDV1Aplha1.SyncStatusCodeSynced {
				return false
			}
		}
		return true

	// Check that the whole ArgoCD App is synced.
	default:
		return argoCDApp.Status.Sync.Status == argoCDV1Aplha1.SyncStatusCodeSynced
	}
}

func setupKubeAgentArgoCDProjectRole(ctx context.Context, projectClient project.ProjectServiceClient, clusterClient client.Client) error {
	// We'll create a project token for the 'kubeaid-agent' role.
	// And save it in the 'argocd-project-role-kubeaid-agent' Kubernetes Secret with token
	// from where KubeAid Agent can pick it up.
	slog.InfoContext(ctx, "Setting up KubeAid Agent ArgoCD project role")

	projectQuery := &project.ProjectQuery{
		Name: constants.ArgoCDProjectKubeAid,
	}

	// Fetch 'kubeaid' project details
	kubeAidProject, err := projectClient.Get(ctx, projectQuery)
	if err != nil {
		return fmt.Errorf("failed fetching KubeAid project details: %w", err)
	}

	description := "Role kubeaid-agent to perform necessary operations via KubeAid Agent"
	policies := []string{
		getKubeAidAgentRolePolicy(
			rbac.ResourceApplications,
			rbac.ActionGet,
			constants.ArgoCDRBACEffectAllow,
		),
		getKubeAidAgentRolePolicy(
			rbac.ResourceApplications,
			rbac.ActionSync,
			constants.ArgoCDRBACEffectAllow,
		),
	}
	projectRole := argoCDV1Aplha1.ProjectRole{
		Name:        constants.ArgoCDRoleKubeAidAgent,
		Description: description,
		Policies:    policies,
		Groups:      []string{constants.ArgoCDRoleKubeAidAgent},
	}
	// Upsert rather than append. ArgoCD rejects an Update that would
	// produce two roles with the same name, so a re-run of bootstrap
	// after the first attempt already added kubeaid-agent (e.g.
	// previous run got past this step but failed later on rook-ceph)
	// would otherwise fail with "role 'kubeaid-agent' already exists"
	// — wedging every subsequent re-run until the operator manually
	// edits the project. Replacing the role in place also picks up
	// any policy/description changes in newer kubeaid-cli releases.
	kubeAidProject.Spec.Roles = upsertProjectRole(kubeAidProject.Spec.Roles, projectRole)

	// Update the project 'kubeaid' by adding role 'kubeaid-agent' details
	projectRequest := &project.ProjectUpdateRequest{
		Project: kubeAidProject,
	}
	_, err = projectClient.Update(ctx, projectRequest)
	if err != nil {
		return fmt.Errorf("failed updating KubeAid project with KubeAid Agent role details: %w", err)
	}

	// Generate the 'kubeaid-agent' project token with no expiry.
	// KubeAid Agent is then uses this token to perform sync operations.
	tokenRequest := &project.ProjectTokenCreateRequest{
		Project: constants.ArgoCDProjectKubeAid,
		Role:    constants.ArgoCDRoleKubeAidAgent,
	}
	tokenResponse, err := projectClient.CreateToken(ctx, tokenRequest)
	if err != nil {
		return fmt.Errorf("failed generating KubeAid project token for KubeAid Agent role: %w", err)
	}

	// Store it in the 'argocd-project-role-kubeaid-agent' Kubernetes Secret in
	// the agent's own namespace (obmondo). The agent reads this Secret at
	// startup to authenticate against the ArgoCD API; ArgoCD itself doesn't
	// need to read it (it only validates the JWT signature when the token is
	// presented), so the Secret legitimately belongs where its consumer runs.
	// Pairs with the kubeaid chart's secret-reader Role in templates/rbac.yaml.
	secretObj := &coreV1.Secret{
		ObjectMeta: metaV1.ObjectMeta{
			Name:      constants.ArgoCDProjectRoleSecretName,
			Namespace: constants.NamespaceObmondo,
			Labels: map[string]string{
				constants.ArgoCDLabelKeyManagedBy: constants.ArgoCDProjectKubeAid,
			},
		},

		StringData: map[string]string{
			"token": tokenResponse.GetToken(),
		},
	}
	err = clusterClient.Create(ctx, secretObj, &client.CreateOptions{})
	if k8sAPIErrors.IsAlreadyExists(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed creating Kubernetes Secret %q in namespace %q: %w",
			constants.ArgoCDProjectRoleSecretName, constants.NamespaceObmondo, err)
	}

	return nil
}

// upsertProjectRole returns roles with role replacing any entry whose
// Name matches role.Name, or with role appended when no match exists.
// Used by setupKubeAgentArgoCDProjectRole to stay idempotent across
// bootstrap re-runs — ArgoCD's project Update rejects duplicate role
// names, so a plain append would wedge every operator who hits a
// downstream failure after the first run added the role.
func upsertProjectRole(roles []argoCDV1Aplha1.ProjectRole, role argoCDV1Aplha1.ProjectRole) []argoCDV1Aplha1.ProjectRole {
	for i, existing := range roles {
		if existing.Name == role.Name {
			roles[i] = role
			return roles
		}
	}
	return append(roles, role)
}

func getKubeAidAgentRolePolicy(resource, action, effect string) string {
	return fmt.Sprintf(
		constants.ArgoCDProjectRolePolicyFmt,
		constants.ArgoCDProjectKubeAid,
		constants.ArgoCDRoleKubeAidAgent,
		resource,
		action,
		constants.ArgoCDProjectKubeAid,
		effect,
	)
}

// Returns the initial ArgoCD admin password.
func getArgoCDAdminPassword(ctx context.Context, clusterClient client.Client) (string, error) {
	argoCDInitialAdminSecret := &coreV1.Secret{}
	err := clusterClient.Get(ctx,
		types.NamespacedName{
			Namespace: constants.NamespaceArgoCD,
			Name:      "argocd-initial-admin-secret",
		},
		argoCDInitialAdminSecret,
	)
	if err != nil {
		return "", fmt.Errorf("failed getting argocd-initial-admin-secret Secret: %w", err)
	}

	argoCDAdminPassword := string(argoCDInitialAdminSecret.Data["password"])
	return argoCDAdminPassword, nil
}

// argoCDHelmValues points helm at the rendered values-argocd.yaml from the
// kubeaid-config fork. This keeps the initial install's values in sync with
// the argocd Application's values (same source of truth, same keys), so
// configs.ssh.knownHosts — derived from user-supplied git.knownHosts in
// general.yaml — is applied before ArgoCD's first root-app clone.
//
// Returns nil if the file doesn't exist (user had no knownHosts and the
// template rendered nothing, or the config step was skipped).
func argoCDHelmValues(ctx context.Context, clusterDir string) *valuesPkg.Options {
	valuesFile := path.Join(clusterDir, "argocd-apps/values-argocd.yaml")
	if _, err := os.Stat(valuesFile); err != nil {
		return nil
	}
	slog.DebugContext(ctx, "Using rendered values-argocd.yaml for ArgoCD install",
		slog.String("path", valuesFile))
	return &valuesPkg.Options{ValueFiles: []string{valuesFile}}
}
