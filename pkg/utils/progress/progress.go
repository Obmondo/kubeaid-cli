// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package progress

import (
	"fmt"
	"os"
	"strings"

	"github.com/schollz/progressbar/v3"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
)

const yubikeyTouchSuffix = " 👉 touch YubiKey"

// Bar is a single-line spinner with a docker-style "log-up"
// behavior: each completed step is promoted to its own permanent
// "✓ <step>" line above the spinner, so the operator gets a
// running checklist of finished work plus a live spinner for the
// step currently in progress.
type Bar struct {
	bar         *progressbar.ProgressBar
	currentDesc string
}

// New creates a spinner-style progress bar (unknown length) with
// the given initial description. The initial description is the
// "header" — it does NOT get a ✓ line of its own; the first
// Describe call starts the first real step.
func New(description string) *Bar {
	bar := progressbar.NewOptions(-1,
		progressbar.OptionSetWriter(os.Stderr),
		progressbar.OptionSetDescription(description),
		progressbar.OptionSpinnerType(14),
		progressbar.OptionClearOnFinish(),
	)
	return &Bar{bar: bar}
}

// Describe advances the spinner to the next step. Any prior step
// is logged up — the spinner clears and the previous caption is
// re-emitted as a permanent "✓ <step>" line above. Top-to-bottom
// the transcript reads as a checklist of done steps with the live
// spinner always at the bottom.
func (b *Bar) Describe(description string) {
	if b.currentDesc != "" && b.currentDesc != description {
		b.logUp(b.currentDesc)
	}
	b.currentDesc = description
	b.bar.Describe(description)
	_ = b.bar.Add(1)
}

// DescribeWithYubiKeyHint is Describe with a "👉 touch YubiKey"
// suffix appended when an SSH agent socket is reachable. Use for
// steps that may block on a hardware-backed SSH signature. The
// suffix appears in the live spinner caption while the step runs;
// once the step completes the suffix is stripped from the logged-
// up "✓ <step>" line — the touch already happened, no further
// action is needed.
func (b *Bar) DescribeWithYubiKeyHint(description string) {
	if os.Getenv(constants.EnvNameSSHAuthSock) != "" {
		description += yubikeyTouchSuffix
	}
	b.Describe(description)
}

// Finish emits the final step's "✓" line and clears the spinner.
func (b *Bar) Finish() {
	if b.currentDesc != "" {
		b.logUp(b.currentDesc)
		b.currentDesc = ""
	}
	_ = b.bar.Finish()
}

// logUp clears the live spinner line and prints a permanent
// "✓ <step>" line in its place. The next Describe call re-renders
// the spinner below it.
func (b *Bar) logUp(desc string) {
	desc = strings.TrimSuffix(desc, yubikeyTouchSuffix)
	_ = b.bar.Clear()
	fmt.Fprintf(os.Stderr, "✓ %s\n", desc)
}
