package azure

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	argoCDV1Alpha1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/kubernetes"
)

// Installs CrossPlane.
// And then provisions required infrastructure for Azure Workload Identity and Disaster Recovery,
// using CrossPlane.
func (*Azure) ProvisionInfrastructure(ctx context.Context) {
	// Install CrossPlane.
	// Then set it up, by installing required Providers, Functions, Compositions and
	// Composite Resource Definitions (XRDs).
	kubernetes.InstallAndSetupCrossplane(ctx)

	// Create Composite Resource (XR) Claims,
	// to provision the Azure Workload Identity and Disaster Recovery infrastructure.
	kubernetes.SyncArgoCDApp(ctx, "infrastructure", []*argoCDV1Alpha1.SyncOperationResource{})

	// Wait until the infrastructure is provisioned.
	// This can be done, by waiting until all the created XRClaims, have their status marked as
	// ready.
	xrClaims := []string{"workloadidentityinfrastructure/default"}
	if config.ParsedGeneralConfig.Cloud.DisasterRecovery != nil {
		xrClaims = append(xrClaims, "disasterrecoveryinfrastructure/default")
	}

	err := wait.PollUntilContextCancel(ctx, 30*time.Second, false,
		func(ctx context.Context) (done bool, err error) {
			for _, xrClaim := range xrClaims {
				output, err := utils.ExecuteCommand(fmt.Sprintf(
					`
            kubectl get %s \
              -n crossplane \
              -o "jsonpath={.status.conditions[?(@.type=='Ready')].status}"
          `,
					xrClaim,
				))
				if (err != nil) || (output != "True") {
					return false, nil
				}
			}
			return true, nil
		},
	)
	assert.AssertErrNil(ctx, err, "Failed waiting for infrastructures to be provisioned")

	slog.InfoContext(ctx, "Required infrastructures have been provisioned using CrossPlane")
}

/*
Retrieves details about the infrastructure provisioned using CrossPlane.

	After CrossPlane has provisioned the infrastructure, CrossPlane provides us the infrastructure
	details in a few ways. Here are the 2 ways we care about :

	(1) Resource specific non-secret details are persisted in the status.atProvider field of the
	    resource object.

	(2) Secret details are persisted to Kubernetes Secrets.
	    REFER : Write connection details requests in CrossPlane Managed Resources, Compositions
	            and Composite Resource (XR) Claims.
*/
func (*Azure) GetInfrastructureDetails(ctx context.Context, clusterClient client.Client) {
	// Retrieve resource specific non-secret details.

	globals.CAPIUAMIClientID = utils.ExecuteCommandOrDie(`
    kubectl get userassignedidentities \
      -l "uami=capi" \
      -n crossplane \
      -o "jsonpath={.items[0].status.atProvider.clientId}"
  `)

	if config.ParsedGeneralConfig.Cloud.DisasterRecovery != nil {
		globals.VeleroUAMIClientID = utils.ExecuteCommandOrDie(`
      kubectl get userassignedidentities \
        -l "uami=velero" \
        -n crossplane \
        -o "jsonpath={.items[0].status.atProvider.clientId}"
    `)
	}

	// Retrieve secret details,
	// from Kubernetes Secrets created by CrossPlane.

	storageAccountConnectionDetailsSecret := &coreV1.Secret{
		ObjectMeta: metaV1.ObjectMeta{
			Name:      "storage-account-details",
			Namespace: constants.NamespaceCrossPlane,
		},
	}

	err := kubernetes.GetKubernetesResource(ctx, clusterClient,
		storageAccountConnectionDetailsSecret,
	)
	assert.AssertErrNil(ctx, err,
		"Failed getting Kubernetes Secret containing storage account connection details",
	)

	encodedAzureStorageAccountAccessKey, ok := storageAccountConnectionDetailsSecret.Data["attribute.primary_access_key"]
	assert.Assert(ctx, ok, "Primary access key not found in storage account connection details")

	globals.AzureStorageAccountAccessKey = string(encodedAzureStorageAccountAccessKey)
}
