package utils

import (
	"fmt"
	"log/slog"
	"path"
	"strings"
	"time"

	"github.com/Obmondo/kubeaid-bootstrap-script/constants"
)

// Installs Sealed Secrets in the underlying Kubernetes cluster.
func InstallSealedSecrets() {
	ExecuteCommandOrDie(`
    helm repo add sealed-secrets https://bitnami-labs.github.io/sealed-secrets
    helm install sealed-secrets sealed-secrets/sealed-secrets --namespace kube-system \
      --set-string fullnameOverride=sealed-secrets-controller \
      --wait
  `)
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
	ExecuteCommandOrDie(`
    helm repo add argo https://argoproj.github.io/argo-helm
    helm install argo-cd argo/argo-cd --namespace argo-cd --create-namespace \
      --set notification.enabled=false --set dex.enabled=false \
      --wait`)
	time.Sleep(time.Second * 20)

	// Port forward ArgoCD server.
	go func() {
		// Sometimes ArgoCD port forward may fail with this error :
		//
		// error copying from remote stream to local connection: readfrom tcp6 [::1]:8080->[::1]:34908:
		// write tcp6 [::1]:8080->[::1]:34908: write: broken pipe
		//
		// In that case we want to re-establish the port-forwarding.
		for {
			output := ExecuteCommand(`
        pkill kubectl -9
        kubectl port-forward svc/argo-cd-argocd-server -n argo-cd 8080:443
      `, false)
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
    argocd login localhost:8080 --username admin --password $ARGOCD_PASSWORD --insecure`)

	// Create the root ArgoCD App.
	slog.Info("Creating and syncing root ArgoCD app")
	rootArgoCDAppPath := path.Join(clusterDir, "argocd-apps/templates/root.yaml")
	ExecuteCommandOrDie(fmt.Sprintf("kubectl apply -f %s", rootArgoCDAppPath))
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

	ExecuteCommandOrDie(fmt.Sprintf(
		"argocd app sync argo-cd/capi-cluster --resource operator.cluster.x-k8s.io:InfrastructureProvider:%s",
		infrastructureProviderName,
	))

	capiClusterNamespace := GetCapiClusterNamespace()
	// Wait for the infrastructure specific CRDs to be installed and pod to be running.
	for {
		if output := ExecuteCommandOrDie(fmt.Sprintf("kubectl get pods -n %s", capiClusterNamespace)); !strings.Contains(output, "No resources found") {
			podStatus := ExecuteCommandOrDie(fmt.Sprintf("kubectl get pods -n %s -o jsonpath='{.items[0].status.phase}'", capiClusterNamespace))
			if podStatus == "Running" {
				break
			}
		}

		slog.Info("Waiting for the capi-cluster pod to come up", slog.String("namespace", capiClusterNamespace))
		time.Sleep(5 * time.Second)
	}
}
