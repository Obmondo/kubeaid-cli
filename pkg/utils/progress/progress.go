// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package progress

import (
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/schollz/progressbar/v3"
	"golang.org/x/crypto/ssh/agent"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
)

const yubikeyTouchSuffix = " 👉 touch YubiKey"

// Bar is a single-line spinner with docker-style "log-up" + a tree
// of indented sub-steps. The transcript reads as:
//
//	   ├─ Created Network
//	   ├─ Registered HCloud SSH key
//	   └─ Created NAT Gateway
//	✓ Provisioning Hetzner infrastructure
//	⠴ Detecting Git authentication method  [0s]
//
// Major steps log up to "✓ <step>" on transition; sub-steps stream
// underneath the live spinner as they happen, with the last sub-
// step before each major-step transition redrawn from "├─" to "└─"
// so the tree closes correctly.
//
// A zero-value Bar{} (returned by FromCtx for contexts with no bar
// attached) is silently a no-op — every method nil-guards so test
// code and library callers don't have to care.
type Bar struct {
	bar         *progressbar.ProgressBar
	currentDesc string
	lastSubstep string
	needsTouch  bool

	// hasYubiKey is the cached "is a YubiKey-backed identity loaded
	// in the SSH agent" check, run once at New(). Gates the touch
	// hint so a software-key-only agent (or no card plugged in)
	// doesn't trigger spurious "👉 touch YubiKey" prompts.
	hasYubiKey bool
}

// New creates a spinner-style progress bar (unknown length) with
// the given header. The header is the whole-run label; it does NOT
// get a "✓" line of its own — the first Describe call starts the
// first real step.
func New(description string) *Bar {
	bar := progressbar.NewOptions(-1,
		progressbar.OptionSetWriter(os.Stderr),
		progressbar.OptionSetDescription(description),
		progressbar.OptionSpinnerType(14),
		progressbar.OptionClearOnFinish(),
	)
	return &Bar{
		bar:        bar,
		hasYubiKey: detectYubiKeyInAgent(),
	}
}

// detectYubiKeyInAgent dials $SSH_AUTH_SOCK and returns true if any
// loaded identity has "cardno:" in its comment — the standard
// OpenSSH-agent / scdaemon marker for a smartcard-backed key
// (which the operator must touch when signing). False when there's
// no agent, no identities, or only software-backed identities.
//
// Run once at Bar construction; the result is cached and re-used
// by every RequestYubiKeyTouch call to avoid hammering the agent.
func detectYubiKeyInAgent() bool {
	sock := os.Getenv(constants.EnvNameSSHAuthSock)
	if sock == "" {
		return false
	}
	conn, err := net.Dial("unix", sock)
	if err != nil {
		return false
	}
	defer conn.Close()
	identities, err := agent.NewClient(conn).List()
	if err != nil {
		return false
	}
	for _, k := range identities {
		if strings.Contains(k.Comment, "cardno:") {
			return true
		}
	}
	return false
}

// Describe advances the spinner to the next major step. The new
// step's header line ("✓ <step>") is printed immediately so any
// sub-steps that stream below are visually nested under it. When
// transitioning, the previous step's last sub-step is redrawn from
// "├─" to "└─" so the tree closes; no separate close line is
// emitted (the next "✓ <step>" header serves as the implicit
// boundary).
//
// Top-to-bottom the final transcript reads as:
//
//	✓ Provisioning Hetzner infrastructure
//	   ├─ Created Hetzner Network
//	   └─ Created NAT Gateway
//	✓ Creating management cluster
//	   └─ ...
//
// No-op for repeat Describe calls with the same description.
func (b *Bar) Describe(description string) {
	if b == nil || b.bar == nil {
		return
	}
	if description == b.currentDesc {
		return
	}
	if b.currentDesc != "" {
		b.closeSubstepTree()
	}
	b.currentDesc = description
	_ = b.bar.Clear()
	fmt.Fprintf(os.Stderr, "✓ %s\n", description)
	b.refreshCaption()
	_ = b.bar.Add(1)
}

// Substep prints "   ├─ <text>" indented under the active major
// step. Sub-step lines stream in real time; the spinner re-renders
// underneath them on the next bar update. The last Substep before
// the next Describe is redrawn as "└─" so the tree closes.
func (b *Bar) Substep(text string) {
	if b == nil || b.bar == nil {
		return
	}
	_ = b.bar.Clear()
	fmt.Fprintf(os.Stderr, "   ├─ %s\n", text)
	b.lastSubstep = text
}

// RequestYubiKeyTouch surfaces the " 👉 touch YubiKey" hint in the
// live spinner caption — operators on a YubiKey/SSH-agent setup
// otherwise have no signal that the spinner is paused waiting for
// a hardware touch. The returned closure clears the hint; pair via
// `defer bar.RequestYubiKeyTouch()()` around the actual SSH op so
// the hint shows only while the touch is genuinely needed.
//
// No-op (returns a no-op closure) when no YubiKey-backed identity
// is loaded in the SSH agent — software-only agents never block
// for a hardware touch and don't need the hint. Card detection is
// cached at Bar construction; plugging in the YubiKey mid-bootstrap
// won't be picked up until next run.
func (b *Bar) RequestYubiKeyTouch() (release func()) {
	if b == nil || b.bar == nil || !b.hasYubiKey {
		return func() {}
	}
	b.needsTouch = true
	b.refreshCaption()
	return func() {
		b.needsTouch = false
		b.refreshCaption()
	}
}

// Finish closes any open sub-step tree and clears the spinner.
// No final "✓" line is emitted — the last Describe's header
// already serves as that block's marker.
func (b *Bar) Finish() {
	if b == nil || b.bar == nil {
		return
	}
	if b.currentDesc != "" {
		b.closeSubstepTree()
		b.currentDesc = ""
	}
	_ = b.bar.Finish()
}

// closeSubstepTree redraws the last sub-step from "├─" to "└─" via
// ANSI cursor-up + clear-line, so the tree branch closes cleanly
// before the next major-step block begins. No-op when there were
// no sub-steps for the current major.
func (b *Bar) closeSubstepTree() {
	if b.lastSubstep == "" {
		return
	}
	_ = b.bar.Clear()
	// Cursor is at the start of the cleared spinner line; the last
	// sub-step row is one above. Move up, clear it, rewrite as the
	// closing "└─" branch.
	fmt.Fprintf(os.Stderr, "\r\033[F\033[K   └─ %s\n", b.lastSubstep)
	b.lastSubstep = ""
}

// refreshCaption re-renders the spinner caption based on the
// current major step + dynamic touch-hint state. Cheap; idempotent.
func (b *Bar) refreshCaption() {
	caption := b.currentDesc
	if b.needsTouch {
		caption += yubikeyTouchSuffix
	}
	b.bar.Describe(caption)
}
