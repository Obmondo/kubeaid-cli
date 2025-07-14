package azure

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	argoCDV1Alpha1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	"k8s.io/apimachinery/pkg/util/wait"

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
	{
		xrClaims := []string{constants.WorkloadIdentityInfrastructureResourceReference}
		if config.ParsedGeneralConfig.Cloud.DisasterRecovery != nil {
			xrClaims = append(xrClaims,
				constants.DisasterRecoveryInfrastructureResourceReference,
			)
		}

		err := wait.PollUntilContextCancel(ctx,
			20*time.Second,
			false,
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

	// Due to the ToFieldPath patches in the CrossPlane compositions, CrossPlane writes details about
	// the provisioned infrastructure to the corresponding XR claim's status section.
	// Let's get those details, for each XR claim.
	{
		// For the WorkloadIdentityInfrastructure XR claim.

		output := utils.ExecuteCommandOrDie(fmt.Sprintf(
			`
        kubectl get %s \
          -n crossplane \
          -o "jsonpath={.status}"
      `,
			constants.WorkloadIdentityInfrastructureResourceReference,
		))
		err := json.Unmarshal([]byte(output), globals.WorkloadIdentityInfrastructureStatus)
		assert.AssertErrNil(
			ctx,
			err,
			"Failed JSON unmarshalling status",
			slog.String(
				"resource-reference",
				constants.WorkloadIdentityInfrastructureResourceReference,
			),
		)

		// For the DisasterRecoveryInfrastructure XR claim.
		if config.ParsedGeneralConfig.Cloud.DisasterRecovery != nil {
			output := utils.ExecuteCommandOrDie(fmt.Sprintf(
				`
          kubectl get %s \
            -n crossplane \
            -o "jsonpath={.status}"
        `,
				constants.DisasterRecoveryInfrastructureResourceReference,
			))
			err := json.Unmarshal([]byte(output), globals.DisasterRecoveryInfrastructureStatus)
			assert.AssertErrNil(
				ctx,
				err,
				"Failed JSON unmarshalling status",
				slog.String(
					"resource-reference",
					constants.DisasterRecoveryInfrastructureResourceReference,
				),
			)
		}
	}
}
