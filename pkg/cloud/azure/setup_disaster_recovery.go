package azure

import (
	"context"
	"log/slog"

	argoCDV1Alpha1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/kubernetes"
)

// Sets up the provisioned cluster for Disaster Recovery.
func (a *Azure) SetupDisasterRecovery(ctx context.Context) {
	disasterRecoveryConfig := config.ParsedGeneralConfig.Cloud.DisasterRecovery
	assert.AssertNotNil(ctx, disasterRecoveryConfig,
		"No Azure disaster-recovery config provided",
	)

	slog.InfoContext(ctx, "Setting up Disaster Recovery")

	// Sync Azure Workload Identity Webhook, Velero and SealedSecrets ArgoCD Apps.
	argocdAppsToBeSynced := []string{
		"azure-workload-identity-webhook",
		constants.ArgoCDAppVelero,
		"sealed-secrets",
	}
	for _, argoCDApp := range argocdAppsToBeSynced {
		kubernetes.SyncArgoCDApp(ctx, argoCDApp, []*argoCDV1Alpha1.SyncOperationResource{})
	}
}
