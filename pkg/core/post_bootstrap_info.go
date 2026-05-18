// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package core

import (
	"fmt"
	"strings"

	"github.com/mattn/go-runewidth"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
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
func printPostBootstrapNextSteps(keycloakAdminPassword string) {
	if !vpnClusterEnabled() || !managedKeycloakEnabled() {
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
		"       Console   https://" + keycloakDNS + "/auth/admin/",
		"       User      " + constants.KeycloakAdminUsername,
		keycloakPasswordLine(keycloakAdminPassword),
		"       Realm     \"" + realm + "\" → Users → Add user (set password under the Credentials tab)",
		"",
		"  2. Join the NetBird mesh with that user",
		"",
		"       Dashboard https://" + netbirdDNS + "/",
		"       Sign-in   click \"Continue\" → Keycloak (confirms the user works)",
		"       Connect   netbird up --management-url https://" + netbirdDNS,
		"                 (opens a browser for Keycloak OIDC sign-in on first run)",
		"",
	}

	printNextStepsBox("Bootstrap complete — next steps", lines)
}

// keycloakPasswordLine returns the "Password" row of the next-steps
// panel. When keycloakAdminPassword is non-empty it's printed inline
// — the friendly path that works after the control-plane LB's public
// IP is disabled, because the operator doesn't have to reach
// kube-apiserver at all to read it. Empty triggers the kubectl-fetch
// fallback the operator can run from inside the NAT gateway / VPN.
func keycloakPasswordLine(keycloakAdminPassword string) string {
	const prefix = "       Password  "
	if keycloakAdminPassword != "" {
		return prefix + keycloakAdminPassword
	}
	cmd := fmt.Sprintf(
		"kubectl get secret -n %s %s -o jsonpath='{.data.%s}' | base64 -d",
		constants.NamespaceKeycloak,
		constants.SecretNameKeycloakAdmin,
		constants.SecretKeyKeycloakPassword,
	)
	return prefix + "$ " + cmd
}

// printNextStepsBox renders the box and writes it to stdout. Thin
// wrapper over renderNextStepsBox so the formatting itself stays
// pure-and-testable.
func printNextStepsBox(title string, lines []string) {
	fmt.Print(renderNextStepsBox(title, lines))
}

// renderNextStepsBox returns title + lines wrapped in a rounded-corner
// Unicode box, no line-wrapping: the box widens to fit the longest
// line so every line — most importantly the Keycloak password kubectl
// command — stays intact for copy-paste.
//
// Differs from pkg/config/prompt.printBox, which wraps overlong
// content to the terminal width and is right for variable-length
// config summaries but would split the kubectl one-liner here.
func renderNextStepsBox(title string, lines []string) string {
	width := runewidth.StringWidth(title) + 4
	for _, l := range lines {
		if w := runewidth.StringWidth(l); w > width {
			width = w
		}
	}

	// Content is padded to width+1 so the longest line still gets one
	// trailing space inside the box — matches the leading space after
	// ╭─ in the top border, so the visual gutter is symmetric.
	pad := func(s string) string {
		gap := width + 1 - runewidth.StringWidth(s)
		if gap <= 0 {
			return s
		}
		return s + strings.Repeat(" ", gap)
	}

	var b strings.Builder

	// Top border: ╭─ Title ───…─╮
	topFill := width - runewidth.StringWidth(title) - 2
	if topFill < 1 {
		topFill = 1
	}
	fmt.Fprintf(&b, "\n╭─ %s %s╮\n", title, strings.Repeat("─", topFill))

	for _, l := range lines {
		fmt.Fprintf(&b, "│%s│\n", pad(l))
	}

	fmt.Fprintf(&b, "╰%s╯\n\n", strings.Repeat("─", width+1))
	return b.String()
}
