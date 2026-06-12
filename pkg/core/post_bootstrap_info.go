// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package core

import (
	"fmt"
	"strings"
	"time"

	"github.com/mattn/go-runewidth"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
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

	title := "Bootstrap complete — next steps"
	if elapsed > 0 {
		title = "Bootstrap complete in " + formatBootstrapDuration(elapsed) + " — next steps"
	}
	printNextStepsBox(title, lines)
}

// printCertManagerNextSteps prints a post-bootstrap hint when
// cert-manager isn't yet able to issue the cluster's TLS certificates.
// cert-manager is always installed, but kubeaid-cli renders an ACME
// ClusterIssuer only when cluster.acmeEmail is set — and an HTTP-01
// issuer can't satisfy the mesh-only hostnames of a NetBird cluster.
//
// Reads the rendered-issuer inputs from config and delegates the
// what-to-say decision to certManagerNextSteps; prints nothing when
// the issuer is configured and adequate. Independent of the Keycloak
// panel above — this fires on any cluster type, not just VPN.
func printCertManagerNextSteps() {
	cluster := config.ParsedGeneralConfig.Cluster
	title, lines, ok := certManagerNextSteps(
		cluster.ACMEEmail, acmeDNS01Enabled(), netBirdOperatorEnabled(),
	)
	if !ok {
		return
	}
	printNextStepsBox(title, lines)
}

// certManagerNextSteps decides which cert-manager hint (if any) to
// show after bootstrap, returning ok=false when the ClusterIssuer is
// configured and adequate so the caller prints nothing.
//
//   - acmeEmail == "": no ACME ClusterIssuer was rendered, so
//     cert-manager won't issue any certificate. Tell the operator how
//     to turn TLS on.
//
//   - acmeEmail set, DNS-01 unset, mesh cluster: the issuer exists but
//     uses HTTP-01, which Let's Encrypt validates by fetching a token
//     over public HTTP — impossible for a hostname that resolves only
//     inside the NetBird mesh. Point the operator at cluster.acmeDNS01.
//
// Every other state (DNS-01 already set, or HTTP-01 on a cluster whose
// names are publicly reachable) is fine and returns ok=false.
//
// Pure — inputs in, strings out — so the branches are unit-testable
// without touching globals or capturing stdout.
func certManagerNextSteps(acmeEmail string, dns01Set, meshCluster bool) (string, []string, bool) {
	if acmeEmail == "" {
		return "cert-manager — TLS not enabled", certManagerNoIssuerLines(), true
	}

	if !dns01Set && meshCluster {
		return "cert-manager — mesh certs need DNS-01", certManagerMeshHTTP01Lines(), true
	}

	return "", nil, false
}

// certManagerNoIssuerLines is the body of the "TLS not enabled" hint:
// cert-manager is running but no ACME issuer was rendered. Example
// values use acme.com placeholders the operator is expected to swap.
func certManagerNoIssuerLines() []string {
	return []string{
		"",
		"  cert-manager is installed, but no ACME ClusterIssuer was",
		"  rendered — so it won't issue any TLS certificates yet.",
		"",
		"  Add a contact email to general.yaml:",
		"",
		"      cluster:",
		"        acmeEmail: ops@acme.com",
		"",
		"  Mesh-only or wildcard hostnames need DNS-01 (HTTP-01 can't",
		"  validate names that don't resolve publicly) — also add the",
		"  solver to general.yaml and the token to secrets.yaml:",
		"",
		"      cluster:",
		"        acmeDNS01:",
		"          provider: cloudflare",
		"          dnsZones: [k8s.acme.com]",
		"",
		"      # secrets.yaml",
		"      acme:",
		"        cloudflareApiToken: <token>",
		"",
		"  then re-render and sync:",
		"",
		"      $ kubeaid-cli cluster bootstrap --configs-directory <dir>",
		"",
	}
}

// certManagerMeshHTTP01Lines is the body of the "mesh certs need
// DNS-01" hint: an HTTP-01 issuer exists but the cluster exposes
// services over the NetBird mesh, whose hostnames Let's Encrypt can't
// reach to validate.
func certManagerMeshHTTP01Lines() []string {
	return []string{
		"",
		"  This cluster exposes services over the NetBird mesh, but its",
		"  ClusterIssuer uses HTTP-01. Let's Encrypt validates HTTP-01 by",
		"  fetching a token over public HTTP, which it can't do for a",
		"  mesh-only hostname — those certificates will stay pending.",
		"",
		"  Switch to DNS-01: add the solver to general.yaml and the",
		"  token to secrets.yaml:",
		"",
		"      cluster:",
		"        acmeDNS01:",
		"          provider: cloudflare",
		"          dnsZones: [k8s.acme.com]",
		"",
		"      # secrets.yaml",
		"      acme:",
		"        cloudflareApiToken: <token>",
		"",
		"  then re-render and sync:",
		"",
		"      $ kubeaid-cli cluster bootstrap --configs-directory <dir>",
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
