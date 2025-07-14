package kubernetes

import (
	"context"

	argoCDV1Alpha1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
)

/*
Installs and sets up CrossPlane.

	Syncs the crossplane, crossplane-providers-and-functions and crossplane-compositions ArgoCD
	Apps one by one.


	NOTE : We need to sync crossplane deployment itself, then the crossplane providers and functions,
	       and finally the crossplane compositions.
	       I tried packaging everything in a single Helm chart, and using ArgoCD sync waves. But,
	       that didn't work. I'll retry later,
*/
func InstallAndSetupCrossplane(ctx context.Context) {
	argoCDAppsToBeSynced := []string{
		"crossplane",
		"crossplane-providers-and-functions",
		"crossplane-compositions",
	}
	for _, argoCDApp := range argoCDAppsToBeSynced {
		SyncArgoCDApp(ctx, argoCDApp, []*argoCDV1Alpha1.SyncOperationResource{})
	}
}
