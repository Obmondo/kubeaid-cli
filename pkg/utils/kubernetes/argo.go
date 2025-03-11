package kubernetes

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path"
	"strings"
	"time"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
	"github.com/argoproj/argo-cd/v2/pkg/apiclient"
	"github.com/argoproj/argo-cd/v2/pkg/apiclient/application"
	"github.com/argoproj/argo-cd/v2/pkg/apiclient/project"
	"github.com/argoproj/argo-cd/v2/pkg/apiclient/session"
	argoCDV1Aplha1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	"github.com/aws/aws-sdk-go-v2/aws"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	coreV1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
		Version:     "7.8.7",
		Namespace:   "argo-cd",
		ReleaseName: "argo-cd",
		Values: map[string]interface{}{
			"notification": map[string]interface{}{
				"enabled": false,
			},
			"dex": map[string]interface{}{
				"enabled": false,
			},
		},
	})

	// Port-forward ArgoCD and create ArgoCD client.
	argoCDClient := NewArgoCDClient(ctx, kubeClient)

	// Create ArgoCD Application client.
	globals.ArgoCDApplicationClientCloser, globals.ArgoCDApplicationClient = argoCDClient.NewApplicationClientOrDie()

	{
		// Create ArgoCD Project client.
		argoCDProjectClientCloser, argoCDProjectClient := argoCDClient.NewProjectClientOrDie()
		defer argoCDProjectClientCloser.Close()

		// Create the kubeaid ArgoCD Project, under which all the ArgoCD Apps will be.
		CreateArgoCDProject(ctx, argoCDProjectClient, constants.ArgoCDProjectKubeAid)
	}

	// Create the Kubernetes Secret, which ArgoCD will use to access the KubeAid config repository.
	argoCDRepoSecretPath := path.Join(clusterDir, "sealed-secrets/argo-cd/kubeaid-config.yaml")
	utils.ExecuteCommandOrDie(fmt.Sprintf("kubectl apply -f %s", argoCDRepoSecretPath))

	// Create the root ArgoCD App.
	{
		rootArgoCDAppPath := path.Join(clusterDir, "argocd-apps/templates/root.yaml")
		utils.ExecuteCommandOrDie(fmt.Sprintf("kubectl apply -f %s", rootArgoCDAppPath))

		slog.Info("Created root ArgoCD app")
	}
}

// Port-forwards the ArgoCD server and creates an ArgoCD client.
// Returns the ArgoCD client.
func NewArgoCDClient(ctx context.Context, kubeClient client.Client) apiclient.Client {
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
	assert.AssertErrNil(ctx, err, "Failed creating session using ArgoCD client")
	defer argoCDClientSessionCloser.Close()

	// Retrieve ArgoCD admin password.
	argoCDInitialAdminSecret := &coreV1.Secret{}
	err = kubeClient.Get(ctx,
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

	return argoCDClient
}

// Tries to create an ArgoCD Project with the given name.
// Skips if the ArgoCD Project already exists.
// Panics on failure.
func CreateArgoCDProject(ctx context.Context,
	argoCDProjectClient project.ProjectServiceClient,
	name string,
) {
	_, err := argoCDProjectClient.Create(ctx, &project.ProjectCreateRequest{
		Project: &argoCDV1Aplha1.AppProject{
			ObjectMeta: v1.ObjectMeta{
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
				ClusterResourceWhitelist:   []v1.GroupKind{{Group: "*", Kind: "*"}},
				NamespaceResourceWhitelist: []v1.GroupKind{{Group: "*", Kind: "*"}},
			},
		},
	})
	if err != nil {
		gRPCResponseStatus, ok := status.FromError(err)
		if ok && (gRPCResponseStatus.Code() == codes.AlreadyExists) {
			slog.InfoContext(ctx, "Skipped creating kubeaid ArgoCD project, since it already exists")
			return
		}

		assert.AssertErrNil(ctx, err, "Failed creating kubeaid ArgoCD Project")
	}

	slog.InfoContext(ctx, "Created kubeaid ArgoCD project")
}

// Recreates the ArgoCD Application client by port-forwarding the ArgoCD server.
// If the clusterClient is not provided (is nil), then it picks up the KUBECONFIG envionment
// variable and constructs the cluster client by itself.
func RecreateArgoCDApplicationClient(ctx context.Context, clusterClient client.Client) {
	// Construct the cluster client, if not provided.
	if clusterClient == nil {
		kubeconfigPath := os.Getenv(constants.EnvNameKubeconfig)
		clusterClient = MustCreateKubernetesClient(ctx, kubeconfigPath)
	}

	// Port-forward ArgoCD and create ArgoCD client.
	argoCDClient := NewArgoCDClient(ctx, clusterClient)

	// Create ArgoCD Application client.
	globals.ArgoCDApplicationClientCloser, globals.ArgoCDApplicationClient = argoCDClient.NewApplicationClientOrDie()
}

// Lists and syncs all the ArgoCD Apps.
func SyncAllArgoCDApps(ctx context.Context) {
	slog.InfoContext(ctx, "Syncing all ArgoCD Apps....")

	// Sync the root ArgoCD App first, so any uncreated ArgoCD Apps get created.
	SyncArgoCDApp(ctx, constants.ArgoCDAppRoot, []*argoCDV1Aplha1.SyncOperationResource{})

	// Sync each ArgoCD App.
	{
		response, err := globals.ArgoCDApplicationClient.List(ctx, &application.ApplicationQuery{})
		assert.AssertErrNil(ctx, err, "Failed listing ArgoCD apps")

		for _, item := range response.Items {
			SyncArgoCDApp(ctx, item.Name, []*argoCDV1Aplha1.SyncOperationResource{})
		}
	}
}

// Syncs the ArgoCD App (if not synced already).
// If the resources array is empty, then the whole ArgoCD App is synced. Otherwise, only the
// specified resources.
func SyncArgoCDApp(ctx context.Context, name string, resources []*argoCDV1Aplha1.SyncOperationResource) {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("app-name", name),
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
		SyncOptions: &application.SyncOptions{
			Items: []string{
				"CreateNamespace=true",
			},
		},
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
	if name == constants.ArgoCDAppKubePrometheus {
		applicationSyncRequest.SyncOptions.Items = append(applicationSyncRequest.SyncOptions.Items, "ServerSideApply=true")
	}

	for {
		_, err := globals.ArgoCDApplicationClient.Sync(ctx, applicationSyncRequest)
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
	var (
		argoCDApp *argoCDV1Aplha1.Application
		err       error
	)
	// We need a retrial mechanism, because when we sync the argo-cd ArgoCD App, the ArgoCD pod may
	// get restarted, which will cause a failure. Then, we need to again port-forward the ArgoCD
	// server and completely reconstruct the ArgoCD Application client.
	for {
		// Get the ArgoCD App.
		argoCDApp, err = globals.ArgoCDApplicationClient.Get(context.Background(), &application.ApplicationQuery{
			Name:         &name,
			Project:      []string{constants.ArgoCDProjectKubeAid},
			AppNamespace: aws.String(constants.NamespaceArgoCD),
		})
		if err == nil {
			break
		}

		slog.ErrorContext(ctx, "Failed getting ArgoCD App. Retrying after 10 seconds....", logger.Error(err))
		time.Sleep(10 * time.Second)

		// Port-forward the ArgoCD server pod and recreate the ArgoCD Application client.
		RecreateArgoCDApplicationClient(ctx, nil)
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
