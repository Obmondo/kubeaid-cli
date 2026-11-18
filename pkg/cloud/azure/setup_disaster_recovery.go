// Copyright 2025 Obmondo
// SPDX-License-Identifier: Apache-2.0

package azure

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	argoCDV1Alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/globals"
)

// syncArgoCDAppFn syncs through the CLI's cluster.
var syncArgoCDAppFn = func(ctx context.Context,
	name string,
	resources []*argoCDV1Alpha1.SyncOperationResource,
) error {
	if globals.Cluster == nil {
		return errors.New("no cluster to sync ArgoCD apps through")
	}
	return globals.Cluster.SyncArgoCDApp(ctx, name, resources)
}

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
