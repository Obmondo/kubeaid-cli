// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	coreV1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
)

// netBirdOperatorSecretName + namespace match the chart's
// secretKeyRef on the netbird-operator Deployment — the operator
// mounts NB_API_KEY from this Secret at startup and falls into
// CreateContainerConfigError when it doesn't exist.
const (
	netBirdOperatorSecretNamespace = "netbird"
	netBirdOperatorSecretName      = "netbird-mgmt-api-key"
	netBirdOperatorSecretKey       = "NB_API_KEY"
)

// awaitNetBirdOperatorToken prints instructions for the operator to
// create a NetBird service-user PAT, then blocks until the matching
// Secret exists in the cluster. Returns nil once the Secret is there
// so the caller can proceed to DisableControlPlaneLBPublicInterface.
//
// keycloakAdminPassword is the live Keycloak admin password (read by the
// caller before the control-plane LB is disabled). It's surfaced in a
// "create your Keycloak login first" box printed just before the NetBird
// instructions, since signing in to the NetBird dashboard is Keycloak SSO
// — see printKeycloakUserSetupForNetBird. Empty triggers the same
// kubectl-fetch fallback the final next-steps panel uses.
//
// Rationale for blocking here, instead of just printing and moving
// on: the netbird-operator ships a MutatingWebhookConfiguration on
// Pods with failurePolicy: Fail. Without NB_API_KEY the operator
// pod stays in CreateContainerConfigError, the webhook target is
// unreachable, and every cluster-wide Pod create fails. Once we
// disable the LB public interface, debugging that state requires
// SSH-jumping through the NAT gateway. Better to discover-and-fix
// before lockdown.
//
// No-op when the cluster doesn't host the netbird-operator at all
// (workload clusters without a keycloak block). Runs on BOTH cluster
// shapes that deploy the operator — VPN clusters (Mgmt is local) and
// workload clusters (Mgmt is the parent VPN's) — the kbm-obmondo-com
// bootstrap proved the workload shape hits the exact same
// CreateContainerConfigError + webhook outage when the Secret is
// missing.
//
// When secrets.yaml carries netbird.apiKey, the sealed
// netbird-mgmt-api-key Secret lands via the secrets app sync and the
// existence check below passes without pausing — this await is the
// interactive fallback, not the primary path.
func awaitNetBirdOperatorToken(
	ctx context.Context,
	clusterClient client.Client,
	keycloakAdminPassword string,
) error {
	if !netBirdOperatorEnabled() {
		return nil
	}

	exists, err := netBirdOperatorSecretExists(ctx, clusterClient)
	if err != nil {
		return fmt.Errorf("checking %s/%s: %w",
			netBirdOperatorSecretNamespace, netBirdOperatorSecretName, err)
	}
	if exists {
		slog.InfoContext(ctx, "NetBird operator API-key Secret already present, skipping prompt",
			slog.String("namespace", netBirdOperatorSecretNamespace),
			slog.String("name", netBirdOperatorSecretName),
		)
		return nil
	}

	// Signing in to the NetBird dashboard (to mint the service-user PAT in
	// the steps below) is Keycloak SSO — so the operator needs a Keycloak
	// realm user first. Print the Keycloak create-user instructions BEFORE
	// the NetBird steps. No-op on clusters without a managed Keycloak.
	printKeycloakUserSetupForNetBird(keycloakAdminPassword)

	printNetBirdOperatorInstructions(netbirdDashboardHost())

	return waitForNetBirdOperatorSecret(ctx, clusterClient)
}

// printKeycloakUserSetupForNetBird renders the "create your Keycloak login
// first" panel shown immediately before the NetBird operator API-key
// instructions. The NetBird dashboard login is Keycloak SSO, so the
// operator must have a realm user before they can sign in to mint the
// service-user PAT.
//
// No-op unless this cluster hosts a managed Keycloak (VPN cluster with
// managed Keycloak) — only then does the admin console + admin credential
// this panel surfaces exist locally. keycloakAdminPassword is the live
// admin password read before the control-plane LB was disabled; empty
// falls back to the kubectl-fetch command (same logic as the final panel).
func printKeycloakUserSetupForNetBird(keycloakAdminPassword string) {
	if !vpnClusterEnabled() || !managedKeycloakEnabled() {
		return
	}
	cluster := config.ParsedGeneralConfig.Cluster
	if cluster.Keycloak == nil || cluster.Keycloak.DNS == "" {
		return
	}

	lines := []string{
		"",
		"  Sign in to Keycloak admin and create a user, then use that user to",
		"  sign in to the NetBird dashboard below (NetBird login is Keycloak SSO).",
		"",
	}
	lines = append(lines,
		keycloakAdminLoginLines(cluster.Keycloak.DNS, cluster.Keycloak.Realm, keycloakAdminPassword)...,
	)
	lines = append(lines, "")

	printNextStepsBox("Create your Keycloak login first", lines)
}

// netbirdDashboardHost returns the NetBird dashboard hostname for the
// instructions panel: cluster.netbird.dns on VPN clusters, the
// netbird.<base> Keycloak-DNS convention on workload clusters, and a
// placeholder when neither derives.
func netbirdDashboardHost() string {
	cluster := config.ParsedGeneralConfig.Cluster

	if cluster.NetBird != nil && cluster.NetBird.DNS != "" {
		return cluster.NetBird.DNS
	}

	if cluster.Keycloak != nil {
		if host := expectedNetBirdHost(cluster.Keycloak.DNS); host != "" {
			return host
		}
	}

	return "<your-netbird-mgmt-dns>"
}

// netBirdOperatorSecretExists returns true when the Secret is present
// and carries a non-empty NB_API_KEY. An empty value would let the
// operator pod schedule but fail at runtime — same outcome as a
// missing Secret, so we treat it the same.
func netBirdOperatorSecretExists(
	ctx context.Context,
	c client.Client,
) (bool, error) {
	secret := &coreV1.Secret{}
	err := c.Get(ctx, types.NamespacedName{
		Namespace: netBirdOperatorSecretNamespace,
		Name:      netBirdOperatorSecretName,
	}, secret)
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return len(secret.Data[netBirdOperatorSecretKey]) > 0, nil
}

// waitForNetBirdOperatorSecret polls every 10s for the Secret to
// appear with a non-empty NB_API_KEY value. Bounded at 30 minutes to
// keep CI / unattended runs from hanging forever — the operator can
// always restart kubeaid-cli later and we'll pick up where we left
// off (the Secret check at the top is idempotent).
func waitForNetBirdOperatorSecret(
	ctx context.Context,
	c client.Client,
) error {
	const (
		interval = 10 * time.Second
		timeout  = 30 * time.Minute
	)
	deadline := time.Now().Add(timeout)

	for {
		exists, err := netBirdOperatorSecretExists(ctx, c)
		if err != nil {
			slog.WarnContext(ctx, "Failed checking NetBird operator Secret, will retry",
				slog.Any("err", err))
		}
		if exists {
			slog.InfoContext(ctx, "NetBird operator API-key Secret detected, continuing bootstrap")
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf(
				"timed out after %s waiting for Secret %s/%s — see instructions printed above",
				timeout, netBirdOperatorSecretNamespace, netBirdOperatorSecretName,
			)
		}
		select {
		case <-ctx.Done():
			return errors.Join(ctx.Err(), errors.New(
				"bootstrap aborted before NetBird operator Secret was created; "+
					"re-run kubeaid-cli once you've created the Secret to continue"))
		case <-time.After(interval):
		}
	}
}

// printNetBirdOperatorInstructions renders the rounded-border panel
// telling the operator how to mint a service-user PAT and persist it
// as the netbird-mgmt-api-key Secret. Same renderer as the
// post-bootstrap next-steps panel so the visual style stays
// consistent across the bootstrap UX.
func printNetBirdOperatorInstructions(netbirdDNS string) {
	dashboardURL := "https://" + netbirdDNS + "/"

	lines := []string{
		"",
		"  The netbird-operator on this cluster needs a NetBird API token",
		"  (service-user PAT) to start. Without it the operator pod stays in",
		"  CreateContainerConfigError and can't serve its admission webhook.",
		"",
		"  Steps in the NetBird Dashboard:",
		"",
		"    1. Sign in:    " + dashboardURL,
		"    2. Sidebar  →  Team  →  Service Users  →  + Create Service User",
		"          Name:  k8s-operator",
		"          Role:  Admin",
		"    3. From the new user's row  →  ⋮  →  Tokens  →  + Generate Token",
		"          Name:        kubeaid-operator",
		"          Expiration:  pick the longest available (rotation note below)",
		"          Copy the token (shown only once).",
		"",
		"  Then EITHER (preferred — persists in git, survives re-creation):",
		"",
		"    Add it to secrets.yaml and re-run kubeaid-cli:",
		"      netbird:",
		"        apiKey: <paste-token-here>",
		"",
		"  OR create the Secret directly (one-off, this cluster only):",
		"",
		"    kubectl -n " + netBirdOperatorSecretNamespace +
			" create secret generic " + netBirdOperatorSecretName + " \\",
		"      --from-literal=" + netBirdOperatorSecretKey + "='<paste-token-here>'",
		"",
		"  Bootstrap will resume automatically once the Secret exists (polls",
		"  every 10s, gives up after 30 minutes). Ctrl+C to abort if you'd",
		"  rather handle this later — re-running kubeaid-cli picks up here.",
		"",
		"  Rotation: NetBird user PATs cap at 180 days. Service-user PATs",
		"  may allow longer; pick the maximum the UI offers. Plan a calendar",
		"  reminder until upstream supports no-expiry service-user tokens.",
		"",
	}

	printNextStepsBox("NetBird operator API key required", lines)
}
