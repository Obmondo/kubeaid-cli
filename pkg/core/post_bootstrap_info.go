// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package core

import (
	"fmt"
	"strings"
	"time"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/ui"
)

// printPostBootstrapNextSteps renders a "next steps" panel at the
// end of a managed-Keycloak VPN bootstrap. It tells the operator
// how to (1) sign in to Keycloak admin to create the first user,
// and (2) log in to NetBird using that user.
//
// keycloakAdminPassword is the live admin password read from the
// keycloak-admin Secret BEFORE the control-plane LB's public IP
// was disabled. Caller reads it while the cluster is still
// reachable from the operator's machine — once disable runs the
// kubeconfig points at a now-defunct public IP and operators not
// on the NetBird mesh can't fetch the password themselves. When
// the live read failed (empty string) the panel falls back to a
// kubectl command the operator has to run from inside the NAT
// gateway, which is much friendlier-than-broken but still extra
// work.
//
// elapsed is how long bootstrap took, rendered into the panel
// title. Zero (the default) prints the bare "Bootstrap complete"
// title without a duration — useful for callers that don't have
// a meaningful start time (tests, future workload-cluster paths).
//
// Managed-Keycloak VPN clusters get the Keycloak-admin + NetBird
// sign-in steps; every other cluster gets the general panel -
// kubeconfig access, the GitOps loop, day-2 upgrades and the ArgoCD
// dashboard - so no bootstrap ever ends without telling the
// operator what to do next.
//
// Writes directly to stdout (not slog) so the box characters and
// alignment survive — slog handlers would add timestamp prefixes
// to each line and break the box. Called after bar.Finish(), so
// there's no live spinner to clash with.
func printPostBootstrapNextSteps(keycloakAdminPassword string, elapsed time.Duration) {
	lines := vpnClusterNextStepsLines(keycloakAdminPassword)
	if lines == nil {
		lines = generalNextStepsLines()
	}

	title := "Bootstrap complete — next steps"
	if elapsed > 0 {
		title = "Bootstrap complete in " + formatBootstrapDuration(elapsed) + " — next steps"
	}
	ui.PrintNextStepsBox(title, lines)
}

// vpnClusterNextStepsLines returns the managed-Keycloak VPN panel content, or nil when the
// cluster isn't one (the credential it surfaces - the keycloak-admin Secret - only exists
// when kubeaid-cli rendered keycloakx itself).
func vpnClusterNextStepsLines(keycloakAdminPassword string) []string {
	if !config.VPNClusterEnabled() || !config.ManagedKeycloakEnabled() {
		return nil
	}
	cluster := config.ParsedGeneralConfig.Cluster
	if cluster.Keycloak == nil || cluster.NetBird == nil {
		return nil
	}
	keycloakDNS := cluster.Keycloak.DNS
	netbirdDNS := cluster.NetBird.DNS
	realm := cluster.Keycloak.Realm
	if keycloakDNS == "" || netbirdDNS == "" {
		return nil
	}

	lines := []string{
		"",
		"  1. Sign in to Keycloak admin and create a user",
		"",
	}
	lines = append(lines, ui.KeycloakAdminLoginLines(keycloakDNS, realm, keycloakAdminPassword)...)
	lines = append(
		lines,
		"",
		"  2. Join the NetBird mesh with that user",
		"",
		"       Dashboard https://"+netbirdDNS+"/",
		"       Sign-in   click \"Continue\" → Keycloak (confirms the user works)",
		"       Connect   netbird up --management-url https://"+netbirdDNS,
		"                 (opens a browser for Keycloak OIDC sign-in on first run)",
		"",
	)
	return lines
}

// generalNextStepsLines is the closing panel for every non-VPN cluster : how to talk to the
// cluster, where its state lives, and how day-2 changes flow.
func generalNextStepsLines() []string {
	return []string{
		"",
		"  1. Talk to the cluster",
		"",
		"       export KUBECONFIG=" + constants.OutputPathMainClusterKubeconfig,
		"       kubectl get nodes && kubectl get pods -A",
		"",
		"  2. Manage everything via GitOps",
		"",
		"       Cluster state lives in " + config.ParsedGeneralConfig.Forks.KubeaidConfigFork.URL,
		"       Edit → PR → merge : ArgoCD reconciles it into the cluster",
		"",
		"  3. Day-2 Kubernetes upgrades",
		"",
		"       Bump cluster.k8sVersion in general.yaml, then run : kubeaid-cli cluster upgrade",
		"",
		"  4. ArgoCD dashboard",
		"",
		"       kubectl -n argocd port-forward svc/argocd-server 8080:443",
		"       kubectl -n argocd get secret argocd-initial-admin-secret -o jsonpath='{.data.password}' | base64 -d",
		"       Open https://localhost:8080  (user: admin)",
		"",
	}
}

// formatBootstrapDuration renders d as a short, human-friendly
// "Hh Mm Ss" / "Mm Ss" / "Ss" — what an operator wants to glance
// at after a 30-minute run, not Go's stock "30m12.345678901s".
//
// Hours and minutes are elided when zero; seconds are always shown
// so a fast (sub-minute) re-run still has a non-empty figure.
func formatBootstrapDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second

	var b strings.Builder
	if h > 0 {
		fmt.Fprintf(&b, "%dh ", h)
	}
	if m > 0 || h > 0 {
		fmt.Fprintf(&b, "%dm ", m)
	}
	fmt.Fprintf(&b, "%ds", s)
	return b.String()
}
