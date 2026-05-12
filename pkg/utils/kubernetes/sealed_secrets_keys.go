// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package kubernetes

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	appsV1 "k8s.io/api/apps/v1"
	coreV1 "k8s.io/api/core/v1"
	k8sAPIErrors "k8s.io/apimachinery/pkg/api/errors"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
)

const (
	// sealedSecretsActiveKeyLabel is the label sealed-secrets controller
	// uses to mark its active signing/decryption keys. Both the
	// controller's own key generation and the upstream rotation tooling
	// apply this label; matching against it picks up exactly the
	// material we need to copy without including unrelated TLS Secrets
	// that happen to live in the same namespace.
	//
	// See kubeaid/argocd-helm-charts/sealed-secrets/README.md and the
	// upstream bitnami-labs/sealed-secrets docs.
	sealedSecretsActiveKeyLabel = "sealedsecrets.bitnami.com/sealed-secrets-key"
	sealedSecretsActiveKeyValue = "active"

	// sealedSecretsControllerDeploymentName mirrors the Deployment that
	// kubeaid's sealed-secrets chart installs. Used by
	// WaitForSealedSecretsControllerReady — the chart's release name
	// and the Deployment name happen to be the same.
	sealedSecretsControllerDeploymentName = "sealed-secrets-controller"
)

// CopySealedSecretsKeysFromManagement copies every active sealed-secrets
// key Secret from the management cluster's sealed-secrets namespace to
// the main cluster's same namespace. After the copy, the main cluster's
// sealed-secrets controller picks the new key(s) up via its Secret
// watch (typically within a second), adding them to its decryption
// keyring alongside whatever key it generated on first start.
//
// Why we do this on every bootstrap: kubeaid-cli runs kubeseal against
// the *management* controller during the management-cluster phase,
// producing SealedSecret artefacts in kubeaid-config that are encrypted
// with the management controller's key. If the main controller doesn't
// also know that key, those SealedSecrets stay undecryptable forever
// (sealing is non-deterministic, so the per-file kubeaid-sha256 cache
// won't re-seal them on the next run unless the plaintext changed).
//
// Idempotent: re-runs overwrite each copied Secret with the current
// management-side bytes, so a re-bootstrap on top of a partially-
// pivoted cluster converges cleanly. Safe to call when the management
// cluster has zero keys (returns nil, no-op).
//
// Mirrors the DR-restore pattern in pkg/core/setup_cluster.go — same
// shape, different source (live management cluster instead of object-
// storage backup).
func CopySealedSecretsKeysFromManagement(ctx context.Context, mgmt, main client.Client) error {
	var keys coreV1.SecretList
	if err := mgmt.List(ctx, &keys,
		client.InNamespace(constants.NamespaceSealedSecrets),
		client.MatchingLabels{sealedSecretsActiveKeyLabel: sealedSecretsActiveKeyValue},
	); err != nil {
		return fmt.Errorf("listing sealed-secrets keys on management cluster: %w", err)
	}

	if len(keys.Items) == 0 {
		slog.InfoContext(ctx,
			"No active sealed-secrets keys on management cluster — nothing to copy",
		)
		return nil
	}

	for i := range keys.Items {
		src := &keys.Items[i]
		desired := &coreV1.Secret{
			ObjectMeta: metaV1.ObjectMeta{
				Name:      src.Name,
				Namespace: constants.NamespaceSealedSecrets,
				Labels: map[string]string{
					sealedSecretsActiveKeyLabel: sealedSecretsActiveKeyValue,
				},
			},
			Type: src.Type,
			Data: src.Data,
		}

		existing := &coreV1.Secret{}
		err := main.Get(ctx,
			types.NamespacedName{Namespace: desired.Namespace, Name: desired.Name},
			existing,
		)
		switch {
		case k8sAPIErrors.IsNotFound(err):
			if createErr := main.Create(ctx, desired); createErr != nil {
				return fmt.Errorf(
					"creating sealed-secrets key %s/%s on main: %w",
					desired.Namespace, desired.Name, createErr,
				)
			}
			slog.InfoContext(ctx,
				"Copied sealed-secrets key from management to main",
				slog.String("name", desired.Name),
			)
		case err != nil:
			return fmt.Errorf(
				"reading sealed-secrets key %s/%s on main: %w",
				desired.Namespace, desired.Name, err,
			)
		default:
			// Already present — patch data/labels to whatever the
			// management cluster currently has. Preserves any other
			// metadata (annotations, finalizers) the main controller
			// added on top.
			existing.Data = desired.Data
			if existing.Labels == nil {
				existing.Labels = map[string]string{}
			}
			existing.Labels[sealedSecretsActiveKeyLabel] = sealedSecretsActiveKeyValue
			if updateErr := main.Update(ctx, existing); updateErr != nil {
				return fmt.Errorf(
					"updating sealed-secrets key %s/%s on main: %w",
					desired.Namespace, desired.Name, updateErr,
				)
			}
			slog.InfoContext(ctx,
				"Refreshed already-present sealed-secrets key on main",
				slog.String("name", desired.Name),
			)
		}
	}

	return nil
}

// WaitForSealedSecretsControllerReady blocks until the sealed-secrets
// controller Deployment in clusterClient reports at least one Available
// replica matching its desired count. Returns the underlying poll error
// on timeout / context cancel.
//
// Used as a fail-fast diagnostic right after the Helm install returns
// — Helm-install returning success means the manifests were applied,
// not that the controller pod is actually scheduled and serving the
// decryption API. Without this wait, the SealedSecret applies that
// follow (repo-kubeaid-config, etc.) can land before the controller
// is ready to decrypt, and the failure manifests downstream as
// cryptic ArgoCD repo-server errors. Blocking here surfaces "controller
// stuck on node taint / image pull / RBAC" directly.
func WaitForSealedSecretsControllerReady(ctx context.Context, clusterClient client.Client, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, 2*time.Second, timeout, true,
		func(ctx context.Context) (bool, error) {
			dep := &appsV1.Deployment{}
			if err := clusterClient.Get(ctx,
				types.NamespacedName{
					Namespace: constants.NamespaceSealedSecrets,
					Name:      sealedSecretsControllerDeploymentName,
				},
				dep,
			); err != nil {
				if k8sAPIErrors.IsNotFound(err) {
					// Deployment not yet created by Helm — keep polling.
					return false, nil
				}
				return false, fmt.Errorf("reading sealed-secrets controller Deployment: %w", err)
			}
			desired := int32(1)
			if dep.Spec.Replicas != nil {
				desired = *dep.Spec.Replicas
			}
			return dep.Status.AvailableReplicas >= desired && desired > 0, nil
		},
	)
}
