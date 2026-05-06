// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package azure

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	argoCDV1Alpha1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/kubernetes"
)

var createUnstructuredClientFn = kubernetes.CreateUnstructuredClient

type xrClaimRef struct {
	group, version, kind, name string
}

// ProvisionInfrastructure installs CrossPlane and then provisions
// required infrastructure for Azure Workload Identity and Disaster Recovery
// using CrossPlane.
func (a *Azure) ProvisionInfrastructure(ctx context.Context) error {
	// Create Composite Resource (XR) Claims,
	// to provision the Azure Workload Identity and Disaster Recovery infrastructure.
	err := syncArgoCDAppFn(ctx, "infrastructure", []*argoCDV1Alpha1.SyncOperationResource{})
	if err != nil {
		return fmt.Errorf("syncing infrastructure ArgoCD app: %w", err)
	}

	unstructuredClient, err := createUnstructuredClientFn(ctx)
	if err != nil {
		return fmt.Errorf("creating unstructured Kubernetes client: %w", err)
	}

	// Wait until the infrastructure is provisioned.
	// This can be done, by waiting until all the created XRClaims, have their status marked as
	// ready.

	xrClaims := []xrClaimRef{
		{
			group:   "infrastructure.obmondo.com",
			version: "v1alpha1",
			kind:    "WorkloadIdentityInfrastructure",
			name:    "default",
		},
	}
	if config.ParsedGeneralConfig.Cloud.DisasterRecovery != nil {
		xrClaims = append(xrClaims, xrClaimRef{
			group:   "infrastructure.obmondo.com",
			version: "v1alpha1",
			kind:    "DisasterRecoveryInfrastructure",
			name:    "default",
		})
	}

	pollInterval := a.pollInterval
	if pollInterval == 0 {
		pollInterval = time.Minute
	}

	err = wait.PollUntilContextCancel(ctx, pollInterval, false,
		func(ctx context.Context) (done bool, err error) {
			for _, ref := range xrClaims {
				ready, err := xrClaimReady(ctx, unstructuredClient, ref)
				if err != nil || !ready {
					//nolint:nilerr
					return false, nil
				}
			}
			return true, nil
		},
	)
	if err != nil {
		return fmt.Errorf("waiting for infrastructures to be provisioned: %w", err)
	}

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
	if err := deleteRoleAssignmentsByLabel(ctx, unstructuredClient); err != nil {
		return fmt.Errorf("deleting UAMI RoleAssignments: %w", err)
	}

	slog.InfoContext(ctx, "Required infrastructures have been provisioned using CrossPlane")
	return nil
}

func xrClaimReady(ctx context.Context, clusterClient client.Client, ref xrClaimRef) (bool, error) {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   ref.group,
		Version: ref.version,
		Kind:    ref.kind,
	})

	if err := clusterClient.Get(ctx, client.ObjectKey{
		Namespace: constants.NamespaceCrossPlane,
		Name:      ref.name,
	}, obj); err != nil {
		return false, err
	}

	conditions, found, err := unstructured.NestedSlice(obj.Object, "status", "conditions")
	if err != nil || !found {
		return false, err
	}

	for _, condition := range conditions {
		conditionMap, ok := condition.(map[string]interface{})
		if !ok {
			continue
		}
		if conditionMap["type"] == "Ready" && conditionMap["status"] == "True" {
			return true, nil
		}
	}
	return false, nil
}

// deleteRoleAssignmentsByLabel deletes roleassignments.authorization.azure.upbound.io
// resources matching label 'uami in (capi, velero)'.
func deleteRoleAssignmentsByLabel(ctx context.Context, clusterClient client.Client) error {
	roleAssignmentList := &unstructured.UnstructuredList{}
	roleAssignmentList.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "authorization.azure.upbound.io",
		Version: "v1beta1",
		Kind:    "RoleAssignmentList",
	})

	selector, err := labels.Parse("uami in (capi, velero)")
	if err != nil {
		return fmt.Errorf("parsing role assignment label selector: %w", err)
	}
	if err := clusterClient.List(ctx, roleAssignmentList, &client.ListOptions{
		LabelSelector: selector,
	}); err != nil {
		return fmt.Errorf("listing RoleAssignments: %w", err)
	}

	for i := range roleAssignmentList.Items {
		item := &roleAssignmentList.Items[i]
		if err := clusterClient.Delete(ctx, item); err != nil {
			return fmt.Errorf("deleting RoleAssignment %q: %w", item.GetName(), err)
		}
	}
	return nil
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
func (*Azure) GetInfrastructureDetails(ctx context.Context, clusterClient client.Client) error {
	unstructuredClient, err := createUnstructuredClientFn(ctx)
	if err != nil {
		return fmt.Errorf("creating unstructured Kubernetes client: %w", err)
	}

	capiClientID, err := getUAMIClientID(ctx, unstructuredClient, "capi")
	if err != nil {
		return err
	}
	globals.CAPIUAMIClientID = capiClientID

	if config.ParsedGeneralConfig.Cloud.DisasterRecovery != nil {
		veleroClientID, err := getUAMIClientID(ctx, unstructuredClient, "velero")
		if err != nil {
			return err
		}
		globals.VeleroUAMIClientID = veleroClientID
	}

	// Retrieve secret details,
	// from Kubernetes Secrets created by CrossPlane.

	storageAccountConnectionDetailsSecret := &coreV1.Secret{
		ObjectMeta: metaV1.ObjectMeta{
			Name:      "storage-account-details",
			Namespace: constants.NamespaceCrossPlane,
		},
	}

	err = kubernetes.GetKubernetesResource(ctx, clusterClient,
		storageAccountConnectionDetailsSecret,
	)
	if err != nil {
		return fmt.Errorf("getting Kubernetes Secret containing storage account connection details: %w", err)
	}

	encodedAzureStorageAccountAccessKey, ok := storageAccountConnectionDetailsSecret.Data["attribute.primary_access_key"]
	if !ok {
		return fmt.Errorf("primary access key not found in storage account connection details")
	}

	globals.AzureStorageAccountAccessKey = string(encodedAzureStorageAccountAccessKey)
	return nil
}

// getUAMIClientID retrieves the clientId from the status.atProvider field of a
// UserAssignedIdentity resource matching the given uami label value.
func getUAMIClientID(ctx context.Context, clusterClient client.Client, uamiLabel string) (string, error) {
	uamiList := &unstructured.UnstructuredList{}
	uamiList.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "managedidentity.azure.upbound.io",
		Version: "v1beta1",
		Kind:    "UserAssignedIdentityList",
	})

	if err := clusterClient.List(ctx, uamiList,
		client.InNamespace(constants.NamespaceCrossPlane),
		client.MatchingLabels{"uami": uamiLabel},
	); err != nil {
		return "", fmt.Errorf("listing UserAssignedIdentities with uami=%q: %w", uamiLabel, err)
	}

	if len(uamiList.Items) == 0 {
		return "", fmt.Errorf("no UserAssignedIdentity found with uami=%q", uamiLabel)
	}

	clientID, found, err := unstructured.NestedString(uamiList.Items[0].Object,
		"status", "atProvider", "clientId")
	if err != nil {
		return "", fmt.Errorf("reading UserAssignedIdentity clientId with uami=%q: %w", uamiLabel, err)
	}
	if !found || clientID == "" {
		return "", fmt.Errorf("UserAssignedIdentity clientId not found with uami=%q", uamiLabel)
	}
	return clientID, nil
}
