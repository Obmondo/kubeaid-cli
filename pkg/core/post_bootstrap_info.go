// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package core

import (
	"fmt"
	"strings"
	"time"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
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
// No-op when the cluster isn't a VPN cluster with managed Keycloak
// — workload clusters and unmanaged-Keycloak setups don't need the
// admin-login step (the operator already has Keycloak running) and
// the credential the panel surfaces (keycloak-admin Secret) only
// exists when kubeaid-cli rendered keycloakx itself.
//
// Writes directly to stdout (not slog) so the box characters and
// alignment survive — slog handlers would add timestamp prefixes
// to each line and break the box. Called after bar.Finish(), so
// there's no live spinner to clash with.
func printPostBootstrapNextSteps(keycloakAdminPassword string, elapsed time.Duration) {
	if !config.VPNClusterEnabled() || !config.ManagedKeycloakEnabled() {
		return
	}
	cluster := config.ParsedGeneralConfig.Cluster
	if cluster.Keycloak == nil || cluster.NetBird == nil {
		return
	}
	keycloakDNS := cluster.Keycloak.DNS
	netbirdDNS := cluster.NetBird.DNS
	realm := cluster.Keycloak.Realm
	if keycloakDNS == "" || netbirdDNS == "" {
		return
	}

	lines := []string{
		"",
		"  1. Sign in to Keycloak admin and create a user",
		"",
	}
	lines = append(lines, ui.KeycloakAdminLoginLines(keycloakDNS, realm, keycloakAdminPassword)...)
	lines = append(lines,
		"",
		"  2. Join the NetBird mesh with that user",
		"",
		"       Dashboard https://"+netbirdDNS+"/",
		"       Sign-in   click \"Continue\" → Keycloak (confirms the user works)",
		"       Connect   netbird up --management-url https://"+netbirdDNS,
		"                 (opens a browser for Keycloak OIDC sign-in on first run)",
		"",
	)

	title := "Bootstrap complete — next steps"
	if elapsed > 0 {
		title = "Bootstrap complete in " + formatBootstrapDuration(elapsed) + " — next steps"
	}
	ui.PrintNextStepsBox(title, lines)
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
