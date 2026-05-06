// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package azure

import (
	"context"
	"fmt"
	"log/slog"

	argoCDV1Alpha1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/kubernetes"
)

var syncArgoCDAppFn = kubernetes.SyncArgoCDApp

// SetupDisasterRecovery sets up the provisioned cluster for Disaster Recovery.
func (a *Azure) SetupDisasterRecovery(ctx context.Context) error {
	disasterRecoveryConfig := config.ParsedGeneralConfig.Cloud.DisasterRecovery
	if disasterRecoveryConfig == nil {
		return fmt.Errorf("no Azure disaster-recovery config provided")
	}

	slog.InfoContext(ctx, "Setting up Disaster Recovery")

	argocdAppsToBeSynced := []string{
		"azure-workload-identity-webhook",
		constants.ArgoCDAppVelero,
		"sealed-secrets",
	}
	for _, argoCDApp := range argocdAppsToBeSynced {
		if err := syncArgoCDAppFn(ctx, argoCDApp, []*argoCDV1Alpha1.SyncOperationResource{}); err != nil {
			return fmt.Errorf("syncing ArgoCD app %s: %w", argoCDApp, err)
		}
	}

	return nil
}
