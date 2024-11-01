package utils

import (
	"fmt"
	"log/slog"
	"path"
	"strings"
	"time"

	"github.com/Obmondo/kubeaid-bootstrap-script/constants"
)

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
	ReleaseName,
	Namespace string
	Values []string
}

// Installs the given Helm chart (if not already installed).
func HelmInstall(args *HelmInstallArgs) {
	// Skip Helm installation if release already exists.
	output, _ := ExecuteCommand(fmt.Sprintf(
		"helm list -n %s --filter %s | grep %s",
		args.Namespace, args.ReleaseName, args.ReleaseName,
	))
	if output != "" {
		return
	}

	// Add Helm repo.
	ExecuteCommandOrDie(fmt.Sprintf("helm repo add %s %s", args.RepoName, args.RepoURL))

	// Install the Helm chart.
	installationCommand := fmt.Sprintf(
		"helm install %s %s/%s -n %s",
		args.ReleaseName, args.RepoName, args.ChartName, args.Namespace,
	)
	for _, value := range args.Values {
		installationCommand += fmt.Sprintf(" --set %s", value)
	}
	installationCommand += " --create-namespace --wait"
	//
	ExecuteCommandOrDie(installationCommand)
}

// Installs Sealed Secrets in the underlying Kubernetes cluster.
func InstallSealedSecrets() {
	HelmInstall(&HelmInstallArgs{
		RepoName:    "sealed-secrets",
		RepoURL:     "https://bitnami-labs.github.io/sealed-secrets",
		ChartName:   "sealed-secrets",
		Namespace:   "kube-system",
		ReleaseName: "sealed-secrets",
		Values:      []string{"fullnameOverride=sealed-secrets-controller"},
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
func InstallAndSetupArgoCD(clusterDir string) {
	// Install ArgoCD.
	HelmInstall(&HelmInstallArgs{
		RepoName:    "argo-cd",
		RepoURL:     "https://argoproj.github.io/argo-helm",
		ChartName:   "argo-cd",
		Namespace:   "argo-cd",
		ReleaseName: "argo-cd",
		Values:      []string{"notification.enabled=false", "dex.enabled=false"},
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

	// Login to ArgoCD from ArgoCD CLI.
	ExecuteCommandOrDie(`
    ARGOCD_PASSWORD=$(kubectl -n argo-cd get secret argocd-initial-admin-secret -o jsonpath="{.data.password}" | base64 -d)
    argocd login localhost:8080 --username admin --password $ARGOCD_PASSWORD --insecure
	`)

	// Create the root ArgoCD App.
	slog.Info("Creating and syncing root ArgoCD app")
	rootArgoCDAppPath := path.Join(clusterDir, "argocd-apps/templates/root.yaml")
	ExecuteCommandOrDie(fmt.Sprintf("kubectl apply -f %s", rootArgoCDAppPath))
}

// Syncs the ArgoCD App (if not synced already).
func SyncArgoCDApp(name string) {
	// TODO : Skip, if the ArgoCD App is already installed.

	// Sync the ArgoCD app.
	ExecuteCommandOrDie(fmt.Sprintf("argocd app sync argo-cd/%s --server-side", name))
}

// Syncs the Infrastructure Provider component of the CAPI Cluster ArgoCD App and waits for the
// infrastructure specific CRDs to be installed and pod to be running.
func SyncInfrastructureProvider() {
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
	ExecuteCommandOrDie(fmt.Sprintf(
		"argocd app sync argo-cd/capi-cluster --server-side --resource operator.cluster.x-k8s.io:InfrastructureProvider:%s",
		infrastructureProviderName,
	))

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
