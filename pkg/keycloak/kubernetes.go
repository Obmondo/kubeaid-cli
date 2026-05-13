// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package keycloak

import (
	"context"
	"fmt"
)

// KubernetesSpec describes the OIDC client a Kubernetes cluster needs
// in a customer's Keycloak realm so kubelogin can authenticate kubectl
// users against that cluster's kube-apiserver.
//
// One Keycloak realm typically hosts multiple Kubernetes clusters
// (the VPN cluster plus each workload cluster joined to it), each
// with its own `kubernetes-<ClusterName>` client.
type KubernetesSpec struct {
	// Realm is the Keycloak realm name (typically derived from the
	// public DNS — see cluster.keycloak.realm).
	Realm string

	// ClusterName is the Kubernetes cluster's name (cluster.name).
	// The OIDC client created in Keycloak is named
	// `kubernetes-<ClusterName>`.
	ClusterName string

	// RedirectURIs are the redirect URIs for kubelogin's localhost
	// callback. Defaults to defaultKubeloginRedirectURIs when empty.
	RedirectURIs []string

	// DefaultScopes are extra ClientScope names to assign as defaults
	// on the kubernetes client (e.g., the NetBird api scope so the
	// audience mapper covers kubelogin tokens too). May be empty.
	DefaultScopes []string
}

// kubernetesClientIDPrefix is the prefix used when naming the OIDC
// client in Keycloak. The full client ID is `<prefix><ClusterName>`,
// e.g. `kubernetes-acme-vpn`.
const kubernetesClientIDPrefix = "kubernetes-"

// defaultKubeloginRedirectURIs are the two localhost callbacks that
// kubelogin (kubectl OIDC plugin) ships with by default. Most
// operators don't override these.
var defaultKubeloginRedirectURIs = []string{
	"http://localhost:8000",
	"http://localhost:18000",
}

// ReconcileKubernetes upserts the `kubernetes-<ClusterName>` PUBLIC
// PKCE OIDC client in the given realm and assigns any DefaultScopes
// the caller asks for. Idempotent — calling with the same spec a
// second time is a no-op.
//
// The realm and any DefaultScopes referenced here must already
// exist; this function does not create them. The typical caller
// runs ReconcileNetBird (which creates the realm + the netbird
// `api` ClientScope) immediately before, then passes that scope's
// name in DefaultScopes here.
func (r *Reconciler) ReconcileKubernetes(ctx context.Context, spec KubernetesSpec) error {
	if spec.Realm == "" {
		return fmt.Errorf("ReconcileKubernetes: spec.Realm is required")
	}
	if spec.ClusterName == "" {
		return fmt.Errorf("ReconcileKubernetes: spec.ClusterName is required")
	}

	redirects := spec.RedirectURIs
	if len(redirects) == 0 {
		redirects = defaultKubeloginRedirectURIs
	}

	clientID := kubernetesClientIDPrefix + spec.ClusterName

	if _, err := r.ReconcileClient(ctx, spec.Realm, ClientSpec{
		ClientID:            clientID,
		PublicClient:        true,
		StandardFlowEnabled: true,
		RedirectURIs:        redirects,
	}); err != nil {
		return err
	}

	if len(spec.DefaultScopes) > 0 {
		if err := r.AssignClientDefaultScopes(ctx, spec.Realm, clientID, spec.DefaultScopes); err != nil {
			return err
		}
	}

	return nil
}
