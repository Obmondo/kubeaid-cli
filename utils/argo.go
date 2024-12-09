package utils

import (
	"context"
	"fmt"
	"log/slog"
	"path"
	"strings"
	"time"

	"github.com/Obmondo/kubeaid-bootstrap-script/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/utils/logger"
	"github.com/argoproj/argo-cd/v2/pkg/apiclient"
	"github.com/argoproj/argo-cd/v2/pkg/apiclient/application"
	"github.com/argoproj/argo-cd/v2/pkg/apiclient/session"
	argoCDV1Aplha1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	"github.com/aws/aws-sdk-go-v2/aws"
	coreV1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Installs ArgoCD Helm chart and creates the root ArgoCD App.
// Then creates and returns an ArgoCD Application client.
func InstallAndSetupArgoCD(ctx context.Context, clusterDir string, kubeClient client.Client) {
	// Install ArgoCD Helm chart.
	HelmInstall(ctx, &HelmInstallArgs{
		RepoName:    "argo-cd",
		RepoURL:     "https://argoproj.github.io/argo-helm",
		ChartName:   "argo-cd",
		Version:     "7.7.0",
		Namespace:   "argo-cd",
		ReleaseName: "argo-cd",
		Values:      "notification.enabled=false, dex.enabled=false",
	})

	// Create the root ArgoCD App.
	slog.Info("Creating root ArgoCD app")
	rootArgoCDAppPath := path.Join(clusterDir, "argocd-apps/templates/root.yaml")

	ExecuteCommandOrDie(fmt.Sprintf("kubectl apply -f %s", rootArgoCDAppPath))

	createArgoCDApplicationClient(ctx, kubeClient)
}

// Creates and returns an ArgoCD application client.
func createArgoCDApplicationClient(ctx context.Context, kubeClient client.Client) {
	slog.InfoContext(ctx, "Creating ArgoCD application client")

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
	}
	argoCDClient := apiclient.NewClientOrDie(argoCDClientOpts)

	// Create a session using that ArgoCD client.
	argoCDClientSessionCloser, argoCDClientSession, err := argoCDClient.NewSessionClient()
	assert.AssertErrNil(ctx, err, "Failed creating session using ArgoCD client")
	defer argoCDClientSessionCloser.Close()

	// Retrieve ArgoCD admin password.
	argoCDInitialAdminSecret := &coreV1.Secret{}
	err = kubeClient.Get(
		ctx,
		types.NamespacedName{
			Namespace: constants.NamespaceArgoCD,
			Name:      "argocd-initial-admin-secret",
		},
		argoCDInitialAdminSecret,
	)
	assert.AssertErrNil(ctx, err, "Failed getting argocd-initial-admin-secret Secret")
	argoCDAdminPassword := string(argoCDInitialAdminSecret.Data["password"])

	// Retrieve ArgoCD auth token.
	response, err := argoCDClientSession.Create(context.Background(), &session.SessionCreateRequest{
		Username: "admin",
		Password: argoCDAdminPassword,
	})
	assert.AssertErrNil(ctx, err, "Failed retrieving ArgoCD auth token")

	// Recreate ArgoCD client, with auth token.
	argoCDClientOpts.AuthToken = response.Token
	argoCDClient = apiclient.NewClientOrDie(argoCDClientOpts)

	// Create ArgoCD Application client.
	constants.ArgoCDApplicationClientCloser, constants.ArgoCDApplicationClient = argoCDClient.NewApplicationClientOrDie()
}

// Syncs the ArgoCD App (if not synced already).
// If the resources array is empty, then the whole ArgoCD App is synced. Otherwise, only the
// specified resources.
func SyncArgoCDApp(ctx context.Context, name string, resources []*argoCDV1Aplha1.SyncOperationResource) {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("app", name),
	})

	// Skip, if the ArgoCD App is already synced.
	if isArgoCDAppSynced(ctx, name, resources) {
		slog.InfoContext(ctx, "Skipped syncing ArgoCD application")
		return
	}

	// Sync the ArgoCD app.

	slog.InfoContext(ctx, "Syncing ArgoCD application")

	applicationSyncRequest := &application.ApplicationSyncRequest{
		Name:         &name,
		AppNamespace: aws.String(constants.NamespaceArgoCD),
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

	for {
		_, err := constants.ArgoCDApplicationClient.Sync(ctx, applicationSyncRequest)
		if err != nil {
			if strings.Contains(err.Error(), "another operation is already in progress") {
				slog.WarnContext(ctx, "ArgoCD App sync failed. Retrying after some time", logger.Error(err))
				time.Sleep(10 * time.Second)
				continue
			}

			assert.AssertErrNil(ctx, err, "Failed syncing ArgoCD application")
		}

		switch name {
		// Wait for the child ArgoCD Apps to be created.
		case constants.ArgoCDAppRoot:
			slog.InfoContext(ctx, "Sleeping for 10 seconds, waiting for the child ArgoCD Apps to be created")
			time.Sleep(10 * time.Second)
			return

		// Wait for the ArgoCD App to be synced.
		default:
			for {
				if isArgoCDAppSynced(ctx, name, resources) {
					return
				}
				slog.InfoContext(ctx, "Waiting for ArgoCD App to be synced")
				time.Sleep(15 * time.Second)
			}
		}
	}
}

// Returns whether the given ArgoCD App is synced or not.
// If the resources array is empty, then checks whether the whole ArgoCD App is synced. Otherwise,
// only checks for the specified resources.
func isArgoCDAppSynced(ctx context.Context, name string, resources []*argoCDV1Aplha1.SyncOperationResource) bool {
	// Get the ArgoCD App.
	argoCDApp, err := constants.ArgoCDApplicationClient.Get(context.Background(), &application.ApplicationQuery{
		Name:         &name,
		AppNamespace: aws.String(constants.NamespaceArgoCD),
	})
	assert.AssertErrNil(ctx, err, "Failed getting ArgoCD App")

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

	// Check that the whole ArgoCD App is synced.
	default:
		if name != constants.ArgoCDAppVelero {
			return argoCDApp.Status.Sync.Status == argoCDV1Aplha1.SyncStatusCodeSynced
		}

		// In case of Velero ArgoCD App, check that all the resources (except Schedules and Backups)
		// are synced.
		for _, resource := range argoCDApp.Status.Resources {
			if resource.Kind == "Schedule" || resource.Kind == "Backup" {
				continue
			}

			if resource.Status != argoCDV1Aplha1.SyncStatusCodeSynced {
				return false
			}
		}
	}
	panic("unreachable")
}
