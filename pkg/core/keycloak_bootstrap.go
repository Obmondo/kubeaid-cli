// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package core

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	coreV1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/keycloak"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/kubernetes"
)

// Tunables for reconcileNetBirdInKeycloak's login → reconcile retry.
// ArgoCD reports the keycloakx app Healthy as soon as the pod's
// readiness probe passes, but Quarkus' realm-handler registration
// runs after that and can take 30–60s more — during which the token
// endpoint returns EOF. 12 × 5s = ~60s budget covers this comfortably
// without dragging out the bootstrap when Keycloak is already warm.
// Package-level vars so tests can shrink them.
var (
	keycloakReconcileMaxAttempts   = 12
	keycloakReconcileRetryInterval = 5 * time.Second
)

// reconcileNetBirdInKeycloak runs the Keycloak admin-API pass against
// the freshly-synced Keycloak: wait for keycloakx to be Healthy, log
// in as admin against the cluster's public Keycloak DNS using the
// password kubeaid-cli rendered into the keycloak-admin Secret, then
// materialise NetBird's realm-side resources via gocloak.
//
// Runs as the keycloakx after-sync hook — before the netbird app
// syncs — so netbird-management starts against OIDC clients that
// already exist. The keycloak-tls Certificate is waited on by the
// same hook just before this call, so the public URL is reachable
// with a valid cert.
//
// The login → reconcile sequence is retried as a unit: Keycloak's
// admin API can briefly race with the app going Healthy, and the
// Reconcile* calls are idempotent, so re-running is safe.
func reconcileNetBirdInKeycloak(ctx context.Context, clusterClient client.Client) error {
	if err := kubernetes.WaitForArgoCDAppHealthy(ctx, constants.ArgoCDAppKeycloakx); err != nil {
		return fmt.Errorf("waiting for keycloakx app to be Healthy: %w", err)
	}

	adminPassword, err := readSecretValue(ctx, clusterClient,
		constants.NamespaceKeycloak,
		constants.SecretNameKeycloakAdmin,
		constants.SecretKeyKeycloakPassword,
	)
	if err != nil {
		return err
	}

	nbBackendSecret, err := readSecretValue(ctx, clusterClient,
		constants.NamespaceNetBird,
		constants.SecretNameNetBird,
		constants.SecretKeyNetBirdIDPMgmtSecret,
	)
	if err != nil {
		return err
	}

	cluster := config.ParsedGeneralConfig.Cluster
	// The keycloakx Helm chart serves Keycloak under the /auth relative
	// path (pre-17 Keycloak default, preserved by the chart for URL
	// stability); gocloak's basePath needs the /auth suffix.
	baseURL := "https://" + cluster.Keycloak.DNS + "/auth"

	return retryKeycloakReconcile(ctx, func(ctx context.Context) error {
		reconciler, err := keycloak.NewReconciler(ctx, baseURL,
			constants.KeycloakAdminUsername, adminPassword,
		)
		if err != nil {
			return err
		}

		if err := reconciler.ReconcileNetBird(ctx, keycloak.NetBirdSpec{
			Realm:                cluster.Keycloak.Realm,
			NetBirdMgmtURL:       "https://" + cluster.NetBird.DNS,
			NetBirdBackendSecret: nbBackendSecret,
		}); err != nil {
			return err
		}

		// Reconcile the kubernetes-<cluster> OIDC client for kubelogin
		// in the same realm. Inherits the NetBird api scope so
		// kubelogin tokens share the audience mapper, and the
		// groups scope so kube-API RBAC can key off the same
		// Keycloak group memberships NetBird sees.
		return reconciler.ReconcileKubernetes(ctx, keycloak.KubernetesSpec{
			Realm:       cluster.Keycloak.Realm,
			ClusterName: cluster.Name,
			DefaultScopes: []string{
				keycloak.NetBirdAPIScopeName,
				keycloak.NetBirdGroupsScopeName,
			},
		})
	})
}

// retryKeycloakReconcile runs attempt up to keycloakReconcileMaxAttempts
// times, keycloakReconcileRetryInterval apart, returning nil on the
// first success. On exhaustion it returns the last error wrapped with
// the attempt count; a cancelled ctx aborts immediately.
func retryKeycloakReconcile(ctx context.Context, attempt func(context.Context) error) error {
	var lastErr error
	for i := 1; i <= keycloakReconcileMaxAttempts; i++ {
		if lastErr = attempt(ctx); lastErr == nil {
			return nil
		}
		if i == keycloakReconcileMaxAttempts {
			break
		}
		slog.WarnContext(ctx, "Keycloak reconcile attempt failed; retrying",
			slog.Int("attempt", i),
			slog.Int("maxAttempts", keycloakReconcileMaxAttempts),
			slog.Any("err", lastErr),
		)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(keycloakReconcileRetryInterval):
		}
	}
	return fmt.Errorf(
		"reconciling NetBird in Keycloak failed after %d attempts: %w",
		keycloakReconcileMaxAttempts, lastErr,
	)
}

// readSecretValue reads a single key out of a namespaced Secret,
// returning a wrapped error that names the Secret on failure so
// bootstrap log output points at the right resource.
func readSecretValue(
	ctx context.Context,
	clusterClient client.Client,
	namespace, name, key string,
) (string, error) {
	secret := &coreV1.Secret{}
	if err := clusterClient.Get(ctx,
		types.NamespacedName{Namespace: namespace, Name: name},
		secret,
	); err != nil {
		return "", fmt.Errorf("reading secret %s/%s: %w", namespace, name, err)
	}
	v, ok := secret.Data[key]
	if !ok || len(v) == 0 {
		return "", fmt.Errorf(
			"secret %s/%s is missing the %q key", namespace, name, key,
		)
	}
	return string(v), nil
}

// readSecretValueOrEmpty is readSecretValue but returns "" instead
// of an error when the cluster, Secret, or key is missing.
//
// Use case: kubeaid-cli's template-render path runs both pre- and
// post-bootstrap. On the very first render the cluster doesn't
// exist yet, on a re-render the Secret may exist with the key
// already populated by an earlier post-sync patch step. Returning
// "" on miss lets the template emit an empty value the first time
// and the persisted value on every run after.
//
// clusterClient may be nil — same treatment as Secret-not-found.
func readSecretValueOrEmpty(
	ctx context.Context,
	clusterClient client.Client,
	namespace, name, key string,
) string {
	if clusterClient == nil {
		return ""
	}
	secret := &coreV1.Secret{}
	if err := clusterClient.Get(ctx,
		types.NamespacedName{Namespace: namespace, Name: name},
		secret,
	); err != nil {
		return ""
	}
	if v, ok := secret.Data[key]; ok && len(v) > 0 {
		return string(v)
	}
	return ""
}
