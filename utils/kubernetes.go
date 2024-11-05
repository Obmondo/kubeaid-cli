package utils

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"strings"
	"time"

	"github.com/Obmondo/kubeaid-bootstrap-script/constants"
	"github.com/argoproj/argo-cd/v2/pkg/apiclient"
	"github.com/argoproj/argo-cd/v2/pkg/apiclient/application"
	"github.com/argoproj/argo-cd/v2/pkg/apiclient/session"
	argoCDV1Aplha1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/strvals"
)

// Returns whether the given K3d cluster exists or not.
func DoesK3dClusterExist(name string) bool {
	output, _ := ExecuteCommand("k3d cluster list --output json | jq -r '.[].name'")

	clusterNames := strings.Split(strings.TrimSpace(output), "\n")
	for _, clusterName := range clusterNames {
		if clusterName == name {
			return true
		}
	}
	return false
}

// Returns the namespace (capi-cluster / capi-cluster-<customer-id>) where the 'cloud-credentials'
// Kubernetes Secret will exist. This Kubernetes Secret will be used by Cluster API to communicate
// with the underlying cloud provider.
func GetCapiClusterNamespace() string {
	capiClusterNamespace := "capi-cluster"
	if len(constants.ParsedConfig.CustomerID) > 0 {
		capiClusterNamespace = fmt.Sprintf("capi-cluster-%s", constants.ParsedConfig.CustomerID)
	}
	return capiClusterNamespace
}

// Creates the given namespace (if it doesn't already exist).
func CreateNamespace(namespace string) {
	// Skip creation if the namespace already exists.
	output := ExecuteCommandOrDie(fmt.Sprintf("kubectl get namespace %s --ignore-not-found", namespace))
	if output != "" {
		return
	}

	ExecuteCommandOrDie(fmt.Sprintf("kubectl create namespace %s", namespace))
}

type HelmInstallArgs struct {
	RepoURL,
	RepoName,
	ChartName,
	Version,
	ReleaseName,
	Namespace string
	Values string
}

// Installs the given Helm chart (if not already installed).
func HelmInstall(args *HelmInstallArgs) {
	settings := cli.New()

	actionConfig := &action.Configuration{}
	err := actionConfig.Init(settings.RESTClientGetter(), settings.Namespace(), os.Getenv("HELM_DRIVER"), slog.Debug)
	if err != nil {
		slog.Error("Failed initializing Helm action config", slog.Any("error", err))
		os.Exit(1)
	}

	// Skip if the Helm chart is already installed.

	listAction := action.NewList(actionConfig)
	listAction.All = true
	listAction.NoHeaders = true
	listAction.Deployed = true
	listAction.Pending = true
	listAction.Filter = args.ReleaseName

	releases, err := listAction.Run()
	if err != nil {
		slog.Error("Failed listing Helm releases", slog.Any("args", args), slog.Any("error", err))
		os.Exit(1)
	}
	for _, release := range releases {
		if (release.Name == args.ReleaseName) && (release.Namespace == args.Namespace) {
			slog.Info("Skipped installing Helm chart", slog.String("chart", args.ChartName))
			return
		}
	}

	// Install the Helm chart.

	slog.Info("Installing Helm chart....", slog.String("chart", args.ChartName))

	installAction := action.NewInstall(actionConfig)
	installAction.ChartPathOptions = action.ChartPathOptions{
		RepoURL: args.RepoURL,
		Version: args.Version,
	}
	installAction.ReleaseName = args.ReleaseName
	installAction.Namespace = args.Namespace
	installAction.CreateNamespace = true
	installAction.Timeout = 10 * time.Minute
	installAction.Wait = true

	// Determine the path to the Helm chart.
	chartPath, err := installAction.ChartPathOptions.LocateChart(args.ChartName, settings)
	if err != nil {
		slog.Error("Failed locating chart path", slog.Any("args", args), slog.Any("error", err))
		os.Exit(1)
	}

	/*
		Load the chart from that chart path.

		We need to retry, since sometimes on the first try, we get this error :

			looks like args.RepoURL is not a valid chart repository or cannot be reached.
			helm.sh/helm/v3/pkg/repo.FindChartInAuthAndTLSAndPassRepoURL
	*/
	var (
		chart                  *chart.Chart
		maxLoadChartRetryCount = 3
	)
	for currentRetryCount := 1; currentRetryCount <= maxLoadChartRetryCount; currentRetryCount++ {
		chart, err = loader.Load(chartPath)
		if (currentRetryCount == maxLoadChartRetryCount) && (err != nil) {
			slog.Error("Failed loading Helm chart", slog.Any("args", args), slog.Any("error", err))
			os.Exit(1)
		}
	}

	// Parse Helm chart values.
	values, err := strvals.Parse(args.Values)
	if err != nil {
		slog.Error("Failed parsing Helm values", slog.Any("args", args), slog.Any("error", err))
		os.Exit(1)
	}

	// Install the Helm chart.
	if _, err = installAction.Run(chart, values); err != nil {
		slog.Error("Failed installing Helm chart", slog.Any("args", args), slog.Any("error", err))
		os.Exit(1)
	}
}

// Installs Sealed Secrets in the underlying Kubernetes cluster.
func InstallSealedSecrets() {
	HelmInstall(&HelmInstallArgs{
		RepoName:    "sealed-secrets",
		RepoURL:     "https://bitnami-labs.github.io/sealed-secrets",
		ChartName:   "sealed-secrets",
		Version:     "2.16.1",
		Namespace:   "kube-system",
		ReleaseName: "sealed-secrets",
		Values:      "fullnameOverride=sealed-secrets-controller",
	})
}

// Takes the path to a Kubernetes Secret file. It then replaces the contents of that file by
// generating the corresponding Sealed Secret.
func GenerateSealedSecret(secretFilePath string) {
	ExecuteCommandOrDie(fmt.Sprintf(`
		kubeseal \
			--controller-name sealed-secrets-controller --controller-namespace kube-system \
			--secret-file %s --sealed-secret-file %s
		`, secretFilePath, secretFilePath))
}

/*
Does the following :

	(1) Install ArgoCD.

	(2) Then port forwards the ArgoCD server by spinning up a go-routine.
	    NOTE : Kills any existing kubectl process.
	    TODO : Just kill the port-forwarding on port 8080, instead of killing all kubectl processes.

	(3) Logs in to ArgoCD from ArgoCD CLI.

	(4) Creates the root ArgoCD app.
*/
func InstallAndSetupArgoCD(clusterDir string) (application.ApplicationServiceClient, io.Closer) {
	// Install ArgoCD.
	HelmInstall(&HelmInstallArgs{
		RepoName:    "argo-cd",
		RepoURL:     "https://argoproj.github.io/argo-helm",
		ChartName:   "argo-cd",
		Version:     "7.7.0",
		Namespace:   "argo-cd",
		ReleaseName: "argo-cd",
		Values:      "notification.enabled=false, dex.enabled=false",
	})
	time.Sleep(time.Second * 20)

	// Port forward ArgoCD server.
	go func() {
		/*
			Sometimes ArgoCD port forward may fail with this error :

				error copying from remote stream to local connection: readfrom tcp6 [::1]:8080->[::1]:34908:
				write tcp6 [::1]:8080->[::1]:34908: write: broken pipe

			In that case, we want to re-establish the port-forwarding.
		*/
		for {
			output, _ := ExecuteCommand(`
        pkill kubectl -9
        kubectl port-forward svc/argo-cd-argocd-server -n argo-cd 8080:443
      `)
			if !strings.Contains(output, "broken pipe") {
				break
			}

			slog.Info("Retrying port-forwarding ArgoCD server")
		}
	}()
	slog.Info("Waiting for kubectl port-forward to be executed in the other go routine....")
	time.Sleep(time.Second * 10)

	// Create the root ArgoCD App.
	slog.Info("Creating and syncing root ArgoCD app")
	rootArgoCDAppPath := path.Join(clusterDir, "argocd-apps/templates/root.yaml")
	ExecuteCommandOrDie(fmt.Sprintf("kubectl apply -f %s", rootArgoCDAppPath))

	// Create ArgoCD client (without auth token).
	argoCDClientOpts := &apiclient.ClientOptions{
		ServerAddr: "localhost:8080",
		ConfigPath: GetEnv(constants.EnvNameKubeconfig),
		PlainText:  true,
		AuthToken:  "",
	}
	argoCDClient := apiclient.NewClientOrDie(argoCDClientOpts)

	// Create a session using the ArgoCD client.
	// We'll use the admin username and password to obtain an auth token.
	argoCDClientSessionCloser, argoCDClientSession, err := argoCDClient.NewSessionClient()
	if err != nil {
		slog.Error("Failed creating an ArgoCD client session", slog.Any("error", err))
		os.Exit(1)
	}
	defer argoCDClientSessionCloser.Close()
	response, err := argoCDClientSession.Create(context.Background(), &session.SessionCreateRequest{
		Username: "admin",
		Password: ExecuteCommandOrDie("kubectl -n argo-cd get secret argocd-initial-admin-secret -o jsonpath=\"{.data.password}\" | base64 -d"),
	})
	if err != nil {
		slog.Error("Failed creating an ArgoCD client session", slog.Any("error", err))
		os.Exit(1)
	}

	// Recreate ArgoCD client, with auth token.
	argoCDClientOpts.AuthToken = response.Token
	argoCDClient = apiclient.NewClientOrDie(argoCDClientOpts)

	// Create ArgoCD Application client.
	argoCDApplicationClientCloser, argoCDApplicationClient := argoCDClient.NewApplicationClientOrDie()
	return argoCDApplicationClient, argoCDApplicationClientCloser
}

// Syncs the ArgoCD App (if not synced already).
// If the resources array is empty, then the whole ArgoCD Application is synced. Otherwise, only the
// specified resources under the ArgoCD Application are synced.
func SyncArgoCDApp(argoCDApplicationClient application.ApplicationServiceClient, name string, resources []*argoCDV1Aplha1.SyncOperationResource) {
	// Skip, if the ArgoCD App is already synced.
	argoCDApplication, err := argoCDApplicationClient.Get(context.Background(), &application.ApplicationQuery{
		Name:         &name,
		AppNamespace: &constants.NamespaceArgoCD,
	})
	if err != nil {
		slog.Error("Failed getting ArgoCD application", slog.String("name", name), slog.String("namespace", constants.NamespaceArgoCD))
		os.Exit(1)
	}
	if argoCDApplication.Status.Sync.Status == argoCDV1Aplha1.SyncStatusCodeSynced {
		return
	}

	// Sync the ArgoCD app.
	_, err = argoCDApplicationClient.Sync(context.Background(), &application.ApplicationSyncRequest{
		Name:         &name,
		AppNamespace: &constants.NamespaceArgoCD,
		Resources:    resources,
		SyncOptions:  &application.SyncOptions{},
	})
	if err != nil {
		slog.Error("Failed syncing ArgoCD application", slog.String("name", name), slog.String("namespace", constants.NamespaceArgoCD))
		os.Exit(1)
	}
}

// Syncs the Infrastructure Provider component of the CAPI Cluster ArgoCD App and waits for the
// infrastructure specific CRDs to be installed and pod to be running.
func SyncInfrastructureProvider(argoCDApplicationClient application.ApplicationServiceClient) {
	// Determine the name of the Infrastructure Provider component.
	var infrastructureProviderName string
	switch {
	case constants.ParsedConfig.Cloud.AWS != nil:
		infrastructureProviderName = "aws"

	default:
		Unreachable()
	}
	if len(constants.ParsedConfig.CustomerID) > 0 {
		infrastructureProviderName = fmt.Sprintf("aws-%s", constants.ParsedConfig.CustomerID)
	}

	// Sync the Infrastructure Provider component.
	SyncArgoCDApp(argoCDApplicationClient, "capi-cluster", []*argoCDV1Aplha1.SyncOperationResource{
		{
			Group: "operator.cluster.x-k8s.io",
			Kind:  "InfrastructureProvider",
			Name:  infrastructureProviderName,
		},
	})

	capiClusterNamespace := GetCapiClusterNamespace()
	// Wait for the infrastructure specific CRDs to be installed and pod to be running.
	for {
		getPodsOutput := ExecuteCommandOrDie(fmt.Sprintf("kubectl get pods -n %s", capiClusterNamespace))
		if !strings.Contains(getPodsOutput, "No resources found") {
			podStatus := ExecuteCommandOrDie(fmt.Sprintf("kubectl get pods -n %s -o jsonpath='{.items[0].status.phase}'", capiClusterNamespace))
			if podStatus == "Running" {
				break
			}
		}

		slog.Info("Waiting for the capi-cluster pod to come up", slog.String("namespace", capiClusterNamespace))
		time.Sleep(5 * time.Second)
	}
}
