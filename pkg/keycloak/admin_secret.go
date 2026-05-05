// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

// Package keycloak holds helpers for managing a managed-mode
// Keycloak instance from kubeaid-cli — admin credentials, and
// (in subsequent work) realm / client reconciliation via the
// admin API.
package keycloak

import (
	"context"
	"fmt"

	coreV1 "k8s.io/api/core/v1"
	k8sAPIErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GetOrGenerateClientSecret returns the value at secretKey from
// the named cluster Secret if it already exists, otherwise
// generates a fresh random secret. Used for client credentials
// where stability across kubeaid-cli runs is load-bearing — e.g.
// netbird-backend's OIDC client secret, which has to match
// between Keycloak's stored value and NetBird Mgmt's
// envFrom-mounted Secret. Regenerating on every run would drift
// the two apart.
//
// clusterClient may be nil (e.g. before the main cluster's
// kubeconfig is available); in that case a fresh secret is
// returned.
func GetOrGenerateClientSecret(
	ctx context.Context,
	clusterClient client.Client,
	namespace, name, secretKey string,
) (string, error) {
	if clusterClient != nil {
		existing := &coreV1.Secret{}
		err := clusterClient.Get(ctx,
			types.NamespacedName{Namespace: namespace, Name: name},
			existing,
		)
		if err == nil {
			if v, ok := existing.Data[secretKey]; ok && len(v) > 0 {
				return string(v), nil
			}
		} else if !k8sAPIErrors.IsNotFound(err) {
			return "", fmt.Errorf(
				"reading Secret %s/%s: %w", namespace, name, err,
			)
		}
	}

	return generatePassword()
}
