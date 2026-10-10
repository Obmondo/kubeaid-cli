// Copyright 2025 Obmondo
// SPDX-License-Identifier: Apache-2.0

package kubernetes

import (
	"context"

	argoCDV1Alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"

	"github.com/Obmondo/kubeaid-cli/pkg/constants"
)

// InstallAndSetupCrossplane syncs the crossplane and crossplane-provider ArgoCD Apps, in that
// order.
//
// crossplane installs the Crossplane core, whose CRDs the crossplane-provider App needs.
// crossplane-provider then installs the Provider and Function packages, the ProviderConfig, the
// Composite Resource Definitions (XRDs) and the Compositions. The ordering between those is
// expressed by the chart itself, with ArgoCD sync waves inside the one App : ArgoCD holds back
// the ProviderConfig and XRDs until the Provider packages report healthy.
func InstallAndSetupCrossplane(ctx context.Context) error {
	mgr := newGlobalArgoCDAppManager()
	return mgr.installAndSetupCrossplane(ctx)
}

// installAndSetupCrossplane is the testable core of InstallAndSetupCrossplane.
func (m *ArgoCDAppManager) installAndSetupCrossplane(ctx context.Context) error {
	for _, argoCDApp := range []string{
		constants.ArgoCDAppCrossplane,
		constants.ArgoCDAppCrossplaneProvider,
	} {
		if err := m.syncArgoCDApp(ctx, argoCDApp, []*argoCDV1Alpha1.SyncOperationResource{}); err != nil {
			return err
		}
	}
	return nil
}
