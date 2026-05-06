// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package core

import (
	"context"
	"fmt"

	coreV1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/keycloak"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/kubernetes"
)

// reconcileNetBirdInKeycloak orchestrates the post-sync admin-API
// pass against the freshly-installed Keycloak: wait for keycloakx
// to be Healthy, port-forward to its Service, log in as admin
// using the password kubeaid-cli rendered into the keycloak-admin
// Secret, then materialise NetBird's realm-side resources via
// gocloak.
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

	restConfig, err := clientcmd.BuildConfigFromFlags("",
		utils.MustGetEnv(constants.EnvNameKubeconfig),
	)
	if err != nil {
		return fmt.Errorf("loading main cluster kubeconfig: %w", err)
	}

	baseURL, stopForward, err := keycloak.PortForward(ctx, restConfig,
		constants.NamespaceKeycloak,
		constants.ServiceNameKeycloakx,
		constants.ServicePortKeycloakx,
	)
	if err != nil {
		return fmt.Errorf("port-forward to keycloakx Service: %w", err)
	}
	defer stopForward()

	reconciler, err := keycloak.NewReconciler(ctx, baseURL,
		constants.KeycloakAdminUsername, adminPassword,
	)
	if err != nil {
		return err
	}

	nbBackendSecret, err := readSecretValue(ctx, clusterClient,
		constants.NamespaceNetBird,
		constants.SecretNameNetBirdKeycloak,
		constants.SecretKeyNetBirdSecret,
	)
	if err != nil {
		return err
	}

	cluster := config.ParsedGeneralConfig.Cluster
	return reconciler.ReconcileNetBird(ctx, keycloak.NetBirdSpec{
		Realm:                cluster.Keycloak.Realm,
		VPNClusterName:       cluster.Name,
		NetBirdMgmtURL:       "https://" + cluster.NetBird.DNS,
		NetBirdBackendSecret: nbBackendSecret,
	})
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
		return "", fmt.Errorf("reading Secret %s/%s: %w", namespace, name, err)
	}
	v, ok := secret.Data[key]
	if !ok || len(v) == 0 {
		return "", fmt.Errorf(
			"secret %s/%s is missing the %q key", namespace, name, key,
		)
	}
	return string(v), nil
}
