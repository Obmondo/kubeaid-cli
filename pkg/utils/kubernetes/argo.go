package kubernetes

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/argoproj/argo-cd/v2/pkg/apiclient"
	"github.com/argoproj/argo-cd/v2/pkg/apiclient/account"
	"github.com/argoproj/argo-cd/v2/pkg/apiclient/application"
	"github.com/argoproj/argo-cd/v2/pkg/apiclient/certificate"
	"github.com/argoproj/argo-cd/v2/pkg/apiclient/project"
	"github.com/argoproj/argo-cd/v2/pkg/apiclient/session"
	argoCDV1Aplha1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	"github.com/aws/aws-sdk-go-v2/aws"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	coreV1 "k8s.io/api/core/v1"
	k8sAPIErrors "k8s.io/apimachinery/pkg/api/errors"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/git"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
)

// Installs the ArgoCD Helm chart and creates the root ArgoCD App.
// Then creates and returns an ArgoCD Application client.
func InstallAndSetupArgoCD(ctx context.Context, clusterDir string, clusterClient client.Client) {
	slog.InfoContext(ctx, "Installing and setting up ArgoCD")

	/*
	   Install the ArgoCD AppProject CRD.
	   Otherwise, we'll get error while installing the ArgoCD Helm chart, since it tries to create
	   the kubeaid ArgoCD App Project during installation.

	   NOTE : We need to retry, since raw.githubusercontent.com doesn't respond sometimes.
	*/
	for {
		_, err := utils.ExecuteCommand(fmt.Sprintf(
			`
      kubectl apply -f https://raw.githubusercontent.com/argoproj/argo-cd/refs/heads/master/manifests/crds/appproject-crd.yaml

      kubectl label crd appprojects.argoproj.io app.kubernetes.io/managed-by=Helm --overwrite
      kubectl annotate crd appprojects.argoproj.io meta.helm.sh/release-name=%s --overwrite
      kubectl annotate crd appprojects.argoproj.io meta.helm.sh/release-namespace=%s --overwrite
    `,
			constants.ReleaseNameArgoCD,
			constants.NamespaceArgoCD,
		))
		if err == nil {
			break
		}
	}

	// Install the ArgoCD Helm chart.
	{
		argoCDHelmValues := map[string]any{}

		// When the user is an Obmondo customer, KubeAid Agent will get deployed to the cluster.
		// We need to create an ArgoCD account for KubeAid Agent.
		argoCDHelmValues["argo-cd"] = map[string]any{
			"configs": map[string]any{
				"cm": map[string]any{
					"accounts.kubeaid-agent": "apiKey",
				},
			},
		}

		HelmInstall(ctx, &HelmInstallArgs{
			ChartPath:   path.Join(utils.GetKubeAidDir(), "argocd-helm-charts/argo-cd"),
			Namespace:   constants.NamespaceArgoCD,
			ReleaseName: constants.ReleaseNameArgoCD,
			Values:      argoCDHelmValues,
		})
	}

	// Port-forward ArgoCD and create ArgoCD client.
	argoCDClient := NewArgoCDClient(ctx, clusterClient)

	// Create the Kubernetes Secret, which ArgoCD will use to access the KubeAid config repository.
	argoCDRepoSecretPath := path.Join(clusterDir, "sealed-secrets/argocd/kubeaid-config.yaml")
	utils.ExecuteCommandOrDie(fmt.Sprintf("kubectl apply -f %s", argoCDRepoSecretPath))

	// Add CA bundle for accessing customer's git server to ArgoCD.
	if len(config.ParsedGeneralConfig.Git.CABundle) > 0 {
		certClientCloser, certClient := argoCDClient.NewCertClientOrDie()
		defer certClientCloser.Close()

		_, err := certClient.CreateCertificate(ctx, &certificate.RepositoryCertificateCreateRequest{
			Upsert: true,
			Certificates: &argoCDV1Aplha1.RepositoryCertificateList{
				Items: []argoCDV1Aplha1.RepositoryCertificate{{
					ServerName: git.GetCustomerGitServerHostName(ctx),
					CertType:   "https",
					CertData:   config.ParsedGeneralConfig.Git.CABundle,
				}},
			},
		})
		assert.AssertErrNil(ctx, err,
			"Failed adding CA bundle (for accessing customer's git server) to ArgoCD",
		)

		slog.InfoContext(ctx, "Added CA bundle (for accessing customer's git server) to ArgoCD")
	}

	// Create ArgoCD Application client.
	globals.ArgoCDApplicationClientCloser, globals.ArgoCDApplicationClient = argoCDClient.NewApplicationClientOrDie()

	// Create the root ArgoCD App.
	rootArgoCDAppPath := path.Join(clusterDir, "argocd-apps/templates/root.yaml")
	utils.ExecuteCommandOrDie(fmt.Sprintf("kubectl apply -f %s", rootArgoCDAppPath))
	slog.InfoContext(ctx, "Created root ArgoCD app")

	// When the user is an Obmondo customer, KubeAid Agent will get deployed to the cluster.
	// We need to setup the ArgoCD account created for KubeAid Agent.
	if (config.ParsedGeneralConfig.Obmondo != nil) &&
		(config.ParsedGeneralConfig.Obmondo.Monitoring) {

		argoCDAccountClientCloser, argoCDAccountClient := argoCDClient.NewAccountClientOrDie()
		defer argoCDAccountClientCloser.Close()

		setupKubeAgentArgoCDAccount(ctx, argoCDAccountClient, clusterClient)
	}
}

// Port-forwards the ArgoCD server and creates an ArgoCD client.
// Returns the ArgoCD client.
func NewArgoCDClient(ctx context.Context, clusterClient client.Client) apiclient.Client {
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
	argoCDAdminPassword := getArgoCDAdminPassword(ctx, clusterClient)

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
			return
		}

		assert.AssertErrNil(ctx, err, "Failed creating kubeaid ArgoCD Project")
	}

	slog.InfoContext(ctx, "Created KubeAid ArgoCD project")
}

// Recreates the ArgoCD Application client by port-forwarding the ArgoCD server.
// If the clusterClient is not provided (is nil), then it picks up the KUBECONFIG envionment
// variable and constructs the cluster client by itself.
func RecreateArgoCDApplicationClient(ctx context.Context, clusterClient client.Client) {
	// Construct the cluster client, if not provided.
	if clusterClient == nil {
		kubeconfigPath := os.Getenv(constants.EnvNameKubeconfig)
		clusterClient = MustCreateClusterClient(ctx, kubeconfigPath)
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
func SyncArgoCDApp(
	ctx context.Context,
	name string,
	resources []*argoCDV1Aplha1.SyncOperationResource,
) {
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
		applicationSyncRequest.SyncOptions.Items = append(applicationSyncRequest.SyncOptions.Items,
			"ServerSideApply=true",
		)
	}

	for {
		_, err := globals.ArgoCDApplicationClient.Sync(ctx, applicationSyncRequest)
		if err != nil {
			if strings.Contains(err.Error(), "another operation is already in progress") {
				slog.WarnContext(ctx,
					"ArgoCD App sync failed. Retrying after some time",
					logger.Error(err),
				)
				time.Sleep(10 * time.Second)
				continue
			}

			assert.AssertErrNil(ctx, err, "Failed syncing ArgoCD application")
		}

		switch name {
		// Wait for the child ArgoCD Apps to be created.
		case constants.ArgoCDAppRoot:
			slog.InfoContext(ctx,
				"Sleeping for 10 seconds, waiting for the child ArgoCD Apps to be created",
			)
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
func isArgoCDAppSynced(
	ctx context.Context,
	name string,
	resources []*argoCDV1Aplha1.SyncOperationResource,
) bool {
	var (
		argoCDApp *argoCDV1Aplha1.Application
		err       error
	)
	// We need a retrial mechanism, because when we sync the argocd ArgoCD App, the ArgoCD pod may
	// get restarted, which will cause a failure. Then, we need to again port-forward the ArgoCD
	// server and completely reconstruct the ArgoCD Application client.
	for {
		// Get the ArgoCD App.
		argoCDApp, err = globals.ArgoCDApplicationClient.Get(
			context.Background(),
			&application.ApplicationQuery{
				Name:         &name,
				Project:      []string{constants.ArgoCDProjectKubeAid},
				AppNamespace: aws.String(constants.NamespaceArgoCD),
			},
		)
		if err == nil {
			break
		}

		slog.ErrorContext(ctx,
			"Failed getting ArgoCD App. Retrying after 10 seconds....",
			logger.Error(err),
		)
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

func setupKubeAgentArgoCDAccount(ctx context.Context,
	argoCDAccountServiceClient account.AccountServiceClient,
	clusterClient client.Client,
) {
	// During Helm installation, an ArgoCD account for KubeAid Agent got created.
	// Unfortunately, ArgoCD will not auto-generate an initial password for that account.
	// So, we need to do it ourselves. And save it in the 'argocd-initial-kubeaid-agent-secret'
	// Kubernetes Secret, from where KubeAid Agent can pick it up.

	slog.InfoContext(ctx, "Setting up KubeAid Agent ArgoCD account")

	// Generate a random password.
	password := strconv.Itoa(int(time.Now().Unix()))

	// Use it for the KubeAid Agent user.
	_, err := argoCDAccountServiceClient.UpdatePassword(ctx, &account.UpdatePasswordRequest{
		Name:            "kubeaid-agent",
		CurrentPassword: getArgoCDAdminPassword(ctx, clusterClient),
		NewPassword:     password,
	})
	assert.AssertErrNil(ctx, err,
		"Failed generating initial password for KubeAid Agent ArgoCD account",
	)

	// Store it in the 'argocd-initial-kubeaid-agent-secret' Kubernetes Secret.

	secretName := "argocd-initial-kubeaid-agent-secret"

	argoCDInitialKubeAidAgentSecret := &coreV1.Secret{
		ObjectMeta: metaV1.ObjectMeta{
			Name:      secretName,
			Namespace: constants.NamespaceArgoCD,
		},

		StringData: map[string]string{
			"password": password,
		},
	}
	err = clusterClient.Create(ctx, argoCDInitialKubeAidAgentSecret, &client.CreateOptions{})
	if k8sAPIErrors.IsAlreadyExists(err) {
		return
	}
	assert.AssertErrNil(ctx, err,
		"Failed creating Kubernetes Secret",
		slog.String("secret", secretName),
		slog.String("namespace", constants.NamespaceArgoCD),
	)
}

// Returns the initial ArgoCD admin password.
func getArgoCDAdminPassword(ctx context.Context, clusterClient client.Client) string {
	argoCDInitialAdminSecret := &coreV1.Secret{}
	err := clusterClient.Get(ctx,
		types.NamespacedName{
			Namespace: constants.NamespaceArgoCD,
			Name:      "argocd-initial-admin-secret",
		},
		argoCDInitialAdminSecret,
	)
	assert.AssertErrNil(ctx, err, "Failed getting argocd-initial-admin-secret Secret")

	argoCDAdminPassword := string(argoCDInitialAdminSecret.Data["password"])
	return argoCDAdminPassword
}
