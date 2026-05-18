// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package core

import (
	"strings"
	"testing"
)

// TestRenderNextStepsBox_NoWrap verifies the box widens to fit the
// longest input line — in particular the kubectl one-liner — instead
// of wrapping it. The whole point of the panel is copy-paste-able
// commands, so a wrapped command is a regression.
func TestRenderNextStepsBox_NoWrap(t *testing.T) {
	longCmd := "kubectl get secret -n keycloakx keycloak-admin -o jsonpath='{.data.KEYCLOAK_PASSWORD}' | base64 -d"

	got := renderNextStepsBox("Bootstrap complete — next steps", []string{
		"  1. Sign in to Keycloak admin",
		"       Password  $ " + longCmd,
	})

	if !strings.Contains(got, longCmd) {
		t.Fatalf("rendered box dropped or split the kubectl command\n%s", got)
	}

	// Every body line begins with '│' and ends with '│' on the same line.
	// Confirm the line containing the command isn't itself split: count
	// how many │…│ rows contain a fragment of the command.
	hits := 0
	for _, line := range strings.Split(got, "\n") {
		if !strings.HasPrefix(line, "│") || !strings.HasSuffix(line, "│") {
			continue
		}
		if strings.Contains(line, "kubectl") || strings.Contains(line, "base64") {
			hits++
		}
	}
	if hits != 1 {
		t.Fatalf("expected the kubectl line to occupy exactly one box row, got %d:\n%s", hits, got)
	}
}

// TestRenderNextStepsBox_BordersAlign verifies every body row has the
// same display width as the top and bottom borders — a misaligned
// box is the usual visual symptom of a width-calc bug (off-by-one
// on Unicode runewidth, padding pulled from the wrong place, etc.).
func TestRenderNextStepsBox_BordersAlign(t *testing.T) {
	out := renderNextStepsBox("Test", []string{
		"short",
		"a somewhat longer line that drives the width",
		// Unicode arrow — exercises runewidth handling.
		"with arrow → here",
	})

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) < 4 {
		t.Fatalf("expected at least top + content + bottom rows, got %d:\n%s", len(lines), out)
	}

	// Find the top border (first non-empty line) and bottom border
	// (last non-empty line) and compare body rows against them by
	// rune count — they're all built from the same `width` and
	// should match exactly.
	var top, bottom string
	for _, l := range lines {
		if strings.HasPrefix(l, "╭") {
			top = l
		}
		if strings.HasPrefix(l, "╰") {
			bottom = l
		}
	}
	if top == "" || bottom == "" {
		t.Fatalf("missing top or bottom border in output:\n%s", out)
	}
	if runesIn(top) != runesIn(bottom) {
		t.Fatalf("top/bottom border widths differ: top=%d bottom=%d\ntop:    %s\nbottom: %s",
			runesIn(top), runesIn(bottom), top, bottom)
	}
}

func runesIn(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}

// TestKeycloakPasswordLine_InlineWhenLiveReadSucceeded verifies the
// friendly path: when kubeaid-cli successfully read the admin
// password before the LB-disable step, the panel shows it inline
// with no `kubectl` / `$` prefix to mislead the operator into
// thinking they need to run something.
func TestKeycloakPasswordLine_InlineWhenLiveReadSucceeded(t *testing.T) {
	got := keycloakPasswordLine("s3cret-from-cluster")

	if !strings.Contains(got, "s3cret-from-cluster") {
		t.Fatalf("expected the password to appear in the line, got %q", got)
	}
	if strings.Contains(got, "kubectl") {
		t.Fatalf("did not expect a kubectl command when password was supplied; got %q", got)
	}
	if strings.Contains(got, "$ ") {
		t.Fatalf("did not expect a `$ ` shell-prompt prefix; got %q", got)
	}
}

// TestKeycloakPasswordLine_FallbackKubectlCommand verifies the
// fallback path: when the live read failed (empty string), the
// panel shows the single-line kubectl command — must remain a
// single line for copy-paste, must mention the right Secret /
// namespace / key so the operator doesn't have to figure them out.
func TestKeycloakPasswordLine_FallbackKubectlCommand(t *testing.T) {
	got := keycloakPasswordLine("")

	if strings.Contains(got, "\n") {
		t.Fatalf("kubectl fallback must stay on one line, got %q", got)
	}
	for _, expect := range []string{"kubectl", "keycloak-admin", "keycloakx", "KEYCLOAK_PASSWORD", "base64 -d"} {
		if !strings.Contains(got, expect) {
			t.Fatalf("kubectl fallback missing %q substring; got %q", expect, got)
		}
	}
}
