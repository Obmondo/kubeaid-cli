// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package kubernetes

import (
	"context"

	argoCDV1Alpha1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
)

// InstallAndSetupCrossplane syncs the crossplane, crossplane-providers-and-functions and
// crossplane-compositions ArgoCD Apps one by one.
//
// The three apps must be synced in order: crossplane first, then providers/functions, then
// compositions. ArgoCD sync waves inside a single chart were attempted but did not produce the
// required ordering — hence the sequential calls below.
func InstallAndSetupCrossplane(ctx context.Context) error {
	mgr := newGlobalArgoCDAppManager()
	return mgr.installAndSetupCrossplane(ctx)
}

// installAndSetupCrossplane is the testable core of InstallAndSetupCrossplane.
func (m *ArgoCDAppManager) installAndSetupCrossplane(ctx context.Context) error {
	for _, argoCDApp := range []string{
		"crossplane",
		"crossplane-providers-and-functions",
		"crossplane-compositions",
	} {
		if err := m.syncArgoCDApp(ctx, argoCDApp, []*argoCDV1Alpha1.SyncOperationResource{}); err != nil {
			return err
		}
	}
	return nil
}
