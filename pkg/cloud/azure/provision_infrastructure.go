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

	err := wait.PollUntilContextCancel(ctx, time.Minute, false,
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
					//nolint:nilerr
					return false, nil
				}
			}
			return true, nil
		},
	)
	assert.AssertErrNil(ctx, err, "Failed waiting for infrastructures to be provisioned")

	/*
		Recreate the Role Assignments.

		Why do we need to this? Let's pick the WorkloadIdentityInfrastructure Composition to understand
		that.

		We assume that you don't have any existing corresponding infrastructure in Azure.
		So, for the first time, when you create an XR out of that Composition, CrossPlane Azure
		providers will create the required infrastructure one by one.

		This includes, creation of the capi UAMI. But, the catch is, UAMI creation is asynchronous.
		So, Azure will return a canonical Principal ID for that UAMI, which will be used by CrossPlane
		to create the Contributor RoleAssignment for that UAMI.

		Later, when the UAMI creation is complete, Azure returns the actual Principal ID back.
		CrossPlane detects a drift, and tries to change the Principal ID in the RoleAssignment, from
		the canonical to the actual one.
		Since, the Principal ID field of the RoleAssignment is immutable, CrossPlane errors out, not
		being able to create the proper RoleAssignment.

		NOTE : We just delete the RoleAssignments.
		       CrossPlane will recreate the proper RoleAssignments in a few seconds.

		TODOs:

		  (1) Recreate them, only if they're marked as ready but not synced.

		  (2) Wait for the proper RoleAssignments to be created.
	*/
	slog.InfoContext(ctx, "Recreating UAMI RoleAssignments")
	utils.ExecuteCommandOrDie(
		"kubectl delete roleassignments.authorization.azure.upbound.io -l 'uami in (capi, velero)'",
	)

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
