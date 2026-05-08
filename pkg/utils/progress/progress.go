// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package progress

import (
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync/atomic"

	"github.com/schollz/progressbar/v3"
	"golang.org/x/crypto/ssh/agent"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
)

// pausableWriter wraps an io.Writer with a runtime mute switch. While
// muted, Write calls report success but discard the bytes — used to
// silence the progressbar's auto-render goroutine during interactive
// prompts so the spinner doesn't overwrite the prompt line.
type pausableWriter struct {
	inner  io.Writer
	paused atomic.Bool
}

func (w *pausableWriter) Write(p []byte) (int, error) {
	if w.paused.Load() {
		return len(p), nil
	}
	return w.inner.Write(p)
}

const yubikeyTouchActivePrefix = "👉 Tap YubiKey to "

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
	writer      *pausableWriter
	currentDesc string
	lastSubstep string

	// hasYubiKey is the cached "is a YubiKey-backed identity loaded
	// in the SSH agent" check, run once at New(). Gates
	// RequestYubiKeyTouch so a software-key-only agent (or no card
	// plugged in) doesn't trigger spurious touch sub-steps.
	hasYubiKey bool
}

// New creates a spinner-style progress bar (unknown length) with
// the given header. The header is the whole-run label; it does NOT
// get a "✓" line of its own — the first Describe call starts the
// first real step.
func New(description string) *Bar {
	pw := &pausableWriter{inner: os.Stderr}
	bar := progressbar.NewOptions(-1,
		progressbar.OptionSetWriter(pw),
		progressbar.OptionSetDescription(description),
		progressbar.OptionSpinnerType(14),
		progressbar.OptionClearOnFinish(),
	)
	return &Bar{
		bar:        bar,
		writer:     pw,
		hasYubiKey: detectYubiKeyInAgent(),
	}
}

// Pause silences the bar's writes to stderr — including the spinner's
// 100ms auto-render goroutine inside progressbar/v3. Use around
// interactive stdin prompts so the spinner can't overwrite the prompt
// line via its `\r`-anchored re-render. Internal state (counters,
// elapsed time) keeps updating; only the visible output is suppressed.
//
// Pair with Resume. Calling Pause clears the spinner line via a direct
// stderr write so the prompt has a clean line to print into.
func (b *Bar) Pause() {
	if b == nil || b.writer == nil {
		return
	}
	b.writer.paused.Store(true)
	// Bar's own Clear would now be muted by the paused writer. Write
	// the clear-line + CR escape directly to the un-muted underlying
	// stderr so the spinner row is visibly cleared before the prompt
	// prints into it.
	fmt.Fprint(os.Stderr, "\033[2K\r")
}

// Resume re-enables the bar's writes. The spinner re-appears on the
// next render (within ~100ms via the auto-tick goroutine, or sooner
// if a Substep/Describe call triggers a render).
func (b *Bar) Resume() {
	if b == nil || b.writer == nil {
		return
	}
	b.writer.paused.Store(false)
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

// RequestYubiKeyTouch emits a transient "👉 Tap YubiKey to <reason>"
// sub-step while the spinner is paused on a hardware-touch SSH
// signature, then ERASES that line once the SSH op completes —
// the work sub-step that follows ("Cloned X", "Pushed Y") is the
// audit trail. We don't keep a permanent "Touched ✓" line because
// a single major step often does several SSH ops back-to-back; a
// stack of identical "Touched ✓" lines would crowd out the real
// progress without adding info.
//
// reason names what the operator is authorizing — "clone repo",
// "push branch", "fetch updates" — and shows up in the prompt so
// they know what they're approving. Keep it short (a few words);
// the prompt is one substep line and shouldn't wrap.
//
// Pair via `defer bar.RequestYubiKeyTouch("...")()` around the actual
// SSH op. Caveat: the erase assumes no other Substep calls fire
// between Request and the closure call, so bracket as tightly as
// possible around the op — emitting other sub-steps in between
// will cause the erase to target the wrong line.
//
// No-op (returns a no-op closure) when no YubiKey-backed identity
// is loaded in the SSH agent — software-only agents never block
// for a hardware touch. Card detection is cached at Bar
// construction; plugging in the YubiKey mid-bootstrap won't be
// picked up until next run.
func (b *Bar) RequestYubiKeyTouch(reason string) (release func()) {
	if b == nil || b.bar == nil || !b.hasYubiKey {
		return func() {}
	}
	prevSubstep := b.lastSubstep
	b.Substep(yubikeyTouchActivePrefix + reason)
	return func() {
		_ = b.bar.Clear()
		// Cursor is at col 0 of the cleared spinner line. Move
		// up to the "Touching..." substep, blank it; the next
		// bar render lands the spinner at the now-empty
		// position so the touch leaves no permanent trace.
		// Restore the pre-touch lastSubstep so closeSubstepTree's
		// ├─ → └─ redraw still targets the correct line.
		fmt.Fprint(os.Stderr, "\033[F\033[2K\r")
		b.lastSubstep = prevSubstep
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

// refreshCaption clears the spinner caption. The major-step
// header ("✓ <step>") already names the current step at the top
// of the visible block, and sub-steps fill the middle — repeating
// the major-step name on the spinner row is redundant noise.
// Spinner shows only its glyph + elapsed-time counter now.
func (b *Bar) refreshCaption() {
	b.bar.Describe("")
}
