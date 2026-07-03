// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

// Package ui renders operator-facing terminal output shared across the
// bootstrap flow — the rounded next-steps box and the Keycloak admin-login
// rows printed inside it.
package ui

import (
	"fmt"
	"strings"

	"github.com/mattn/go-runewidth"

	"github.com/Obmondo/kubeaid-cli/pkg/constants"
)

// PrintNextStepsBox renders the box and writes it to stdout. Thin wrapper
// over RenderNextStepsBox so the formatting itself stays pure-and-testable.
func PrintNextStepsBox(title string, lines []string) {
	fmt.Print(RenderNextStepsBox(title, lines))
}

// RenderNextStepsBox returns title + lines wrapped in a rounded-corner
// Unicode box, no line-wrapping: the box widens to fit the longest line so
// commands stay intact for copy-paste. Differs from pkg/config/prompt's
// printBox, which wraps overlong content to the terminal width.
func RenderNextStepsBox(title string, lines []string) string {
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

// KeycloakAdminLoginLines returns the console / user / password / realm rows
// for signing in to the Keycloak admin console and adding a user. Shared by
// the final next-steps panel and the pre-NetBird-gate prompt so both render
// identical rows.
func KeycloakAdminLoginLines(keycloakDNS, realm, keycloakAdminPassword string) []string {
	return []string{
		"       Console   https://" + keycloakDNS + "/auth/admin/",
		"       User      " + constants.KeycloakAdminUsername,
		KeycloakPasswordLine(keycloakAdminPassword),
		"       Realm     \"" + realm + "\" → Users → Add user (set password under the Credentials tab)",
	}
}

// KeycloakPasswordLine returns the "Password" row: the live password inline
// when known (readable even after the control-plane LB's public IP is
// disabled), else the kubectl-fetch command.
func KeycloakPasswordLine(keycloakAdminPassword string) string {
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
